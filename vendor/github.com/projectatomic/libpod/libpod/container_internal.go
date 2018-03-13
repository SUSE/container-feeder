package libpod

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/containers/storage"
	"github.com/containers/storage/pkg/archive"
	"github.com/containers/storage/pkg/chrootarchive"
	"github.com/docker/docker/pkg/mount"
	"github.com/docker/docker/pkg/namesgenerator"
	"github.com/docker/docker/pkg/stringid"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-tools/generate"
	"github.com/opencontainers/selinux/go-selinux/label"
	"github.com/pkg/errors"
	crioAnnotations "github.com/projectatomic/libpod/pkg/annotations"
	"github.com/projectatomic/libpod/pkg/chrootuser"
	"github.com/sirupsen/logrus"
	"github.com/ulule/deepcopier"
	"golang.org/x/sys/unix"
)

const (
	// name of the directory holding the artifacts
	artifactsDir = "artifacts"
)

// rootFsSize gets the size of the container's root filesystem
// A container FS is split into two parts.  The first is the top layer, a
// mutable layer, and the rest is the RootFS: the set of immutable layers
// that make up the image on which the container is based.
func (c *Container) rootFsSize() (int64, error) {
	container, err := c.runtime.store.Container(c.ID())
	if err != nil {
		return 0, err
	}

	// Ignore the size of the top layer.   The top layer is a mutable RW layer
	// and is not considered a part of the rootfs
	rwLayer, err := c.runtime.store.Layer(container.LayerID)
	if err != nil {
		return 0, err
	}
	layer, err := c.runtime.store.Layer(rwLayer.Parent)
	if err != nil {
		return 0, err
	}

	size := int64(0)
	for layer.Parent != "" {
		layerSize, err := c.runtime.store.DiffSize(layer.Parent, layer.ID)
		if err != nil {
			return size, errors.Wrapf(err, "getting diffsize of layer %q and its parent %q", layer.ID, layer.Parent)
		}
		size += layerSize
		layer, err = c.runtime.store.Layer(layer.Parent)
		if err != nil {
			return 0, err
		}
	}
	// Get the size of the last layer.  Has to be outside of the loop
	// because the parent of the last layer is "", andlstore.Get("")
	// will return an error.
	layerSize, err := c.runtime.store.DiffSize(layer.Parent, layer.ID)
	return size + layerSize, err
}

// rwSize Gets the size of the mutable top layer of the container.
func (c *Container) rwSize() (int64, error) {
	container, err := c.runtime.store.Container(c.ID())
	if err != nil {
		return 0, err
	}

	// Get the size of the top layer by calculating the size of the diff
	// between the layer and its parent.  The top layer of a container is
	// the only RW layer, all others are immutable
	layer, err := c.runtime.store.Layer(container.LayerID)
	if err != nil {
		return 0, err
	}
	return c.runtime.store.DiffSize(layer.Parent, layer.ID)
}

// The path to the container's root filesystem - where the OCI spec will be
// placed, amongst other things
func (c *Container) bundlePath() string {
	return c.config.StaticDir
}

// Retrieves the path of the container's attach socket
func (c *Container) attachSocketPath() string {
	return filepath.Join(c.runtime.ociRuntime.socketsDir, c.ID(), "attach")
}

// Get PID file path for a container's exec session
func (c *Container) execPidPath(sessionID string) string {
	return filepath.Join(c.state.RunDir, "exec_pid_"+sessionID)
}

// Sync this container with on-disk state and runtime status
// Should only be called with container lock held
// This function should suffice to ensure a container's state is accurate and
// it is valid for use.
func (c *Container) syncContainer() error {
	if err := c.runtime.state.UpdateContainer(c); err != nil {
		return err
	}
	// If runtime knows about the container, update its status in runtime
	// And then save back to disk
	if (c.state.State != ContainerStateUnknown) &&
		(c.state.State != ContainerStateConfigured) {
		oldState := c.state.State
		// TODO: optionally replace this with a stat for the exit file
		if err := c.runtime.ociRuntime.updateContainerStatus(c); err != nil {
			return err
		}
		// Only save back to DB if state changed
		if c.state.State != oldState {
			if err := c.save(); err != nil {
				return err
			}
		}
	}

	if !c.valid {
		return errors.Wrapf(ErrCtrRemoved, "container %s is not valid", c.ID())
	}

	return nil
}

// Make a new container
func newContainer(rspec *spec.Spec, lockDir string) (*Container, error) {
	if rspec == nil {
		return nil, errors.Wrapf(ErrInvalidArg, "must provide a valid runtime spec to create container")
	}

	ctr := new(Container)
	ctr.config = new(ContainerConfig)
	ctr.state = new(containerState)

	ctr.config.ID = stringid.GenerateNonCryptoID()
	ctr.config.Name = namesgenerator.GetRandomName(0)

	ctr.config.Spec = new(spec.Spec)
	deepcopier.Copy(rspec).To(ctr.config.Spec)
	ctr.config.CreatedTime = time.Now()

	ctr.config.ShmSize = DefaultShmSize
	ctr.config.CgroupParent = DefaultCgroupParent

	// Path our lock file will reside at
	lockPath := filepath.Join(lockDir, ctr.config.ID)
	// Grab a lockfile at the given path
	lock, err := storage.GetLockfile(lockPath)
	if err != nil {
		return nil, errors.Wrapf(err, "error creating lockfile for new container")
	}
	ctr.lock = lock

	return ctr, nil
}

// Create container root filesystem for use
func (c *Container) setupStorage() error {
	if !c.valid {
		return errors.Wrapf(ErrCtrRemoved, "container %s is not valid", c.ID())
	}

	if c.state.State != ContainerStateConfigured {
		return errors.Wrapf(ErrCtrStateInvalid, "container %s must be in Configured state to have storage set up", c.ID())
	}

	// Need both an image ID and image name, plus a bool telling us whether to use the image configuration
	if c.config.RootfsImageID == "" || c.config.RootfsImageName == "" {
		return errors.Wrapf(ErrInvalidArg, "must provide image ID and image name to use an image")
	}

	containerInfo, err := c.runtime.storageService.CreateContainerStorage(c.runtime.imageContext, c.config.RootfsImageName, c.config.RootfsImageID, c.config.Name, c.config.ID, c.config.MountLabel)
	if err != nil {
		return errors.Wrapf(err, "error creating container storage")
	}

	c.config.StaticDir = containerInfo.Dir
	c.state.RunDir = containerInfo.RunDir

	artifacts := filepath.Join(c.config.StaticDir, artifactsDir)
	if err := os.MkdirAll(artifacts, 0755); err != nil {
		return errors.Wrapf(err, "error creating artifacts directory %q", artifacts)
	}

	return nil
}

// Tear down a container's storage prior to removal
func (c *Container) teardownStorage() error {
	if !c.valid {
		return errors.Wrapf(ErrCtrRemoved, "container %s is not valid", c.ID())
	}

	if c.state.State == ContainerStateRunning || c.state.State == ContainerStatePaused {
		return errors.Wrapf(ErrCtrStateInvalid, "cannot remove storage for container %s as it is running or paused", c.ID())
	}

	artifacts := filepath.Join(c.config.StaticDir, artifactsDir)
	if err := os.RemoveAll(artifacts); err != nil {
		return errors.Wrapf(err, "error removing artifacts %q", artifacts)
	}

	if err := c.cleanupStorage(); err != nil {
		return errors.Wrapf(err, "failed to cleanup container %s storage", c.ID())
	}

	if err := c.runtime.storageService.DeleteContainer(c.ID()); err != nil {
		return errors.Wrapf(err, "error removing container %s root filesystem", c.ID())
	}

	return nil
}

// Refresh refreshes the container's state after a restart
func (c *Container) refresh() error {
	c.lock.Lock()
	defer c.lock.Unlock()

	if !c.valid {
		return errors.Wrapf(ErrCtrRemoved, "container %s is not valid - may have been removed", c.ID())
	}

	// We need to get the container's temporary directory from c/storage
	// It was lost in the reboot and must be recreated
	dir, err := c.runtime.storageService.GetRunDir(c.ID())
	if err != nil {
		return errors.Wrapf(err, "error retrieving temporary directory for container %s", c.ID())
	}
	c.state.RunDir = dir

	if err := c.runtime.state.SaveContainer(c); err != nil {
		return errors.Wrapf(err, "error refreshing state for container %s", c.ID())
	}

	return nil
}

func (c *Container) export(path string) error {
	mountPoint := c.state.Mountpoint
	if !c.state.Mounted {
		mount, err := c.runtime.store.Mount(c.ID(), c.config.MountLabel)
		if err != nil {
			return errors.Wrapf(err, "error mounting container %q", c.ID())
		}
		mountPoint = mount
		defer func() {
			if err := c.runtime.store.Unmount(c.ID()); err != nil {
				logrus.Errorf("error unmounting container %q: %v", c.ID(), err)
			}
		}()
	}

	input, err := archive.Tar(mountPoint, archive.Uncompressed)
	if err != nil {
		return errors.Wrapf(err, "error reading container directory %q", c.ID())
	}

	outFile, err := os.Create(path)
	if err != nil {
		return errors.Wrapf(err, "error creating file %q", path)
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, input)
	return err
}

// Get path of artifact with a given name for this container
func (c *Container) getArtifactPath(name string) string {
	return filepath.Join(c.config.StaticDir, artifactsDir, name)
}

// Used with Wait() to determine if a container has exited
func (c *Container) isStopped() (bool, error) {
	if !c.locked {
		c.lock.Lock()
		defer c.lock.Unlock()
	}
	err := c.syncContainer()
	if err != nil {
		return true, err
	}
	return c.state.State == ContainerStateStopped, nil
}

// save container state to the database
func (c *Container) save() error {
	if err := c.runtime.state.SaveContainer(c); err != nil {
		return errors.Wrapf(err, "error saving container %s state", c.ID())
	}
	return nil
}

// Internal, non-locking function to stop container
func (c *Container) stop(timeout uint) error {
	logrus.Debugf("Stopping ctr %s with timeout %d", c.ID(), timeout)

	if c.state.State == ContainerStateConfigured ||
		c.state.State == ContainerStateUnknown ||
		c.state.State == ContainerStatePaused {
		return errors.Wrapf(ErrCtrStateInvalid, "can only stop created, running, or stopped containers")
	}

	if err := c.runtime.ociRuntime.stopContainer(c, timeout); err != nil {
		return err
	}

	// Sync the container's state to pick up return code
	if err := c.runtime.ociRuntime.updateContainerStatus(c); err != nil {
		return err
	}

	return c.cleanupStorage()
}

// mountStorage sets up the container's root filesystem
// It mounts the image and any other requested mounts
// TODO: Add ability to override mount label so we can use this for Mount() too
// TODO: Can we use this for export? Copying SHM into the export might not be
// good
func (c *Container) mountStorage() (err error) {
	// Container already mounted, nothing to do
	if c.state.Mounted {
		return nil
	}

	// TODO: generalize this mount code so it will mount every mount in ctr.config.Mounts

	mounted, err := mount.Mounted(c.config.ShmDir)
	if err != nil {
		return errors.Wrapf(err, "unable to determine if %q is mounted", c.config.ShmDir)
	}

	if !mounted {
		shmOptions := fmt.Sprintf("mode=1777,size=%d", c.config.ShmSize)
		if err := unix.Mount("shm", c.config.ShmDir, "tmpfs", unix.MS_NOEXEC|unix.MS_NOSUID|unix.MS_NODEV,
			label.FormatMountLabel(shmOptions, c.config.MountLabel)); err != nil {
			return errors.Wrapf(err, "failed to mount shm tmpfs %q", c.config.ShmDir)
		}
	}

	mountPoint, err := c.runtime.storageService.MountContainerImage(c.ID())
	if err != nil {
		return errors.Wrapf(err, "error mounting storage for container %s", c.ID())
	}
	c.state.Mounted = true
	c.state.Mountpoint = mountPoint

	logrus.Debugf("Created root filesystem for container %s at %s", c.ID(), c.state.Mountpoint)

	defer func() {
		if err != nil {
			if err2 := c.cleanupStorage(); err2 != nil {
				logrus.Errorf("Error unmounting storage for container %s: %v", c.ID(), err)
			}
		}
	}()

	return c.save()
}

// cleanupNetwork unmounts and cleans up the container's network
func (c *Container) cleanupNetwork() error {
	// Stop the container's network namespace (if it has one)
	if err := c.runtime.teardownNetNS(c); err != nil {
		logrus.Errorf("unable cleanup network for container %s: %q", c.ID(), err)
	}

	c.state.NetNS = nil
	c.state.IPs = nil
	c.state.Routes = nil
	return c.save()
}

// cleanupStorage unmounts and cleans up the container's root filesystem
func (c *Container) cleanupStorage() error {
	if !c.state.Mounted {
		// Already unmounted, do nothing
		return nil
	}

	for _, mount := range c.config.Mounts {
		if err := unix.Unmount(mount, unix.MNT_DETACH); err != nil {
			if err != syscall.EINVAL {
				logrus.Warnf("container %s failed to unmount %s : %v", c.ID(), mount, err)
			}
		}
	}

	// Also unmount storage
	if err := c.runtime.storageService.UnmountContainerImage(c.ID()); err != nil {
		return errors.Wrapf(err, "error unmounting container %s root filesystem", c.ID())
	}

	c.state.Mountpoint = ""
	c.state.Mounted = false

	return c.save()
}

// WriteStringToRundir copies the provided file to the runtimedir
func (c *Container) WriteStringToRundir(destFile, output string) (string, error) {
	destFileName := filepath.Join(c.state.RunDir, destFile)
	f, err := os.Create(destFileName)
	if err != nil {
		return "", errors.Wrapf(err, "unable to create %s", destFileName)
	}

	defer f.Close()
	_, err = f.WriteString(output)
	if err != nil {
		return "", errors.Wrapf(err, "unable to write %s", destFileName)
	}
	// Relabel runDirResolv for the container
	if err := label.Relabel(destFileName, c.config.MountLabel, false); err != nil {
		return "", err
	}
	return destFileName, nil
}

type resolvConf struct {
	nameServers   []string
	searchDomains []string
	options       []string
}

// generateResolvConf generates a containers resolv.conf
func (c *Container) generateResolvConf() (string, error) {
	// Copy /etc/resolv.conf to the container's rundir
	resolvPath := "/etc/resolv.conf"

	// Check if the host system is using system resolve and if so
	// copy its resolv.conf
	if _, err := os.Stat("/run/systemd/resolve/resolv.conf"); err == nil {
		resolvPath = "/run/systemd/resolve/resolv.conf"
	}
	orig, err := ioutil.ReadFile(resolvPath)
	if err != nil {
		return "", errors.Wrapf(err, "unable to read %s", resolvPath)
	}
	if len(c.config.DNSServer) == 0 && len(c.config.DNSSearch) == 0 && len(c.config.DNSOption) == 0 {
		return c.WriteStringToRundir("resolv.conf", fmt.Sprintf("%s", orig))
	}

	// Read and organize the hosts /etc/resolv.conf
	resolv := createResolv(string(orig[:]))

	// Populate the resolv struct with user's dns search domains
	if len(c.config.DNSSearch) > 0 {
		resolv.searchDomains = nil
		// The . character means the user doesnt want any search domains in the container
		if !StringInSlice(".", c.config.DNSSearch) {
			resolv.searchDomains = append(resolv.searchDomains, c.Config().DNSSearch...)
		}
	}

	// Populate the resolv struct with user's dns servers
	if len(c.config.DNSServer) > 0 {
		resolv.nameServers = nil
		for _, i := range c.config.DNSServer {
			resolv.nameServers = append(resolv.nameServers, i.String())
		}
	}

	// Populate the resolve struct with the users dns options
	if len(c.config.DNSOption) > 0 {
		resolv.options = nil
		resolv.options = append(resolv.options, c.Config().DNSOption...)
	}
	return c.WriteStringToRundir("resolv.conf", resolv.ToString())
}

// createResolv creates a resolv struct from an input string
func createResolv(input string) resolvConf {
	var resolv resolvConf
	for _, line := range strings.Split(input, "\n") {
		if strings.HasPrefix(line, "search") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				logrus.Debugf("invalid resolv.conf line %s", line)
				continue
			}
			resolv.searchDomains = append(resolv.searchDomains, fields[1:]...)
		} else if strings.HasPrefix(line, "nameserver") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				logrus.Debugf("invalid resolv.conf line %s", line)
				continue
			}
			resolv.nameServers = append(resolv.nameServers, fields[1])
		} else if strings.HasPrefix(line, "options") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				logrus.Debugf("invalid resolv.conf line %s", line)
				continue
			}
			resolv.options = append(resolv.options, fields[1:]...)
		}
	}
	return resolv
}

//ToString returns a resolv struct in the form of a resolv.conf
func (r resolvConf) ToString() string {
	var result string
	// Populate the output string with search domains
	result += fmt.Sprintf("search %s\n", strings.Join(r.searchDomains, " "))
	// Populate the output string with name servers
	for _, i := range r.nameServers {
		result += fmt.Sprintf("nameserver %s\n", i)
	}
	// Populate the output string with dns options
	for _, i := range r.options {
		result += fmt.Sprintf("options %s\n", i)
	}
	return result
}

// generateHosts creates a containers hosts file
func (c *Container) generateHosts() (string, error) {
	orig, err := ioutil.ReadFile("/etc/hosts")
	if err != nil {
		return "", errors.Wrapf(err, "unable to read /etc/hosts")
	}
	hosts := string(orig)
	if len(c.config.HostAdd) > 0 {
		for _, host := range c.config.HostAdd {
			// the host format has already been verified at this point
			fields := strings.Split(host, ":")
			hosts += fmt.Sprintf("%s %s\n", fields[0], fields[1])
		}
	}
	return c.WriteStringToRundir("hosts", hosts)
}

// generateEtcHostname creates a containers /etc/hostname
func (c *Container) generateEtcHostname(hostname string) (string, error) {
	return c.WriteStringToRundir("hostname", hostname)
}

// Generate spec for a container
func (c *Container) generateSpec(resolvPath, hostsPath, hostnamePath string) (*spec.Spec, error) {
	g := generate.NewFromSpec(c.config.Spec)

	// If network namespace was requested, add it now
	if c.config.CreateNetNS {
		g.AddOrReplaceLinuxNamespace(spec.NetworkNamespace, c.state.NetNS.Path())
	}
	// Remove default /etc/shm mount
	g.RemoveMount("/dev/shm")
	// Mount ShmDir from host into container
	shmMnt := spec.Mount{
		Type:        "bind",
		Source:      c.config.ShmDir,
		Destination: "/dev/shm",
		Options:     []string{"rw", "bind"},
	}
	g.AddMount(shmMnt)
	// Bind mount resolv.conf
	resolvMnt := spec.Mount{
		Type:        "bind",
		Source:      resolvPath,
		Destination: "/etc/resolv.conf",
		Options:     []string{"rw", "bind"},
	}
	if !MountExists(g.Mounts(), resolvMnt.Destination) {
		g.AddMount(resolvMnt)
	}
	// Bind mount hosts
	hostsMnt := spec.Mount{
		Type:        "bind",
		Source:      hostsPath,
		Destination: "/etc/hosts",
		Options:     []string{"rw", "bind"},
	}
	if !MountExists(g.Mounts(), hostsMnt.Destination) {
		g.AddMount(hostsMnt)
	}
	// Bind hostname
	hostnameMnt := spec.Mount{
		Type:        "bind",
		Source:      hostnamePath,
		Destination: "/etc/hostname",
		Options:     []string{"rw", "bind"},
	}
	if !MountExists(g.Mounts(), hostnameMnt.Destination) {
		g.AddMount(hostnameMnt)
	}

	// Bind builtin image volumes
	if c.config.ImageVolumes {
		if err := c.addImageVolumes(&g); err != nil {
			return nil, errors.Wrapf(err, "error mounting image volumes")
		}
	}

	if c.config.User != "" {
		if !c.state.Mounted {
			return nil, errors.Wrapf(ErrCtrStateInvalid, "container %s must be mounted in order to translate User field", c.ID())
		}
		uid, gid, err := chrootuser.GetUser(c.state.Mountpoint, c.config.User)
		if err != nil {
			return nil, err
		}
		// User and Group must go together
		g.SetProcessUID(uid)
		g.SetProcessGID(gid)
	}

	// Add shared namespaces from other containers
	if c.config.IPCNsCtr != "" {
		if err := c.addNamespaceContainer(&g, IPCNS, c.config.IPCNsCtr, spec.IPCNamespace); err != nil {
			return nil, err
		}
	}
	if c.config.MountNsCtr != "" {
		if err := c.addNamespaceContainer(&g, MountNS, c.config.MountNsCtr, spec.MountNamespace); err != nil {
			return nil, err
		}
	}
	if c.config.NetNsCtr != "" {
		if err := c.addNamespaceContainer(&g, NetNS, c.config.NetNsCtr, spec.NetworkNamespace); err != nil {
			return nil, err
		}
	}
	if c.config.PIDNsCtr != "" {
		if err := c.addNamespaceContainer(&g, PIDNS, c.config.PIDNsCtr, string(spec.PIDNamespace)); err != nil {
			return nil, err
		}
	}
	if c.config.UserNsCtr != "" {
		if err := c.addNamespaceContainer(&g, UserNS, c.config.UserNsCtr, spec.UserNamespace); err != nil {
			return nil, err
		}
	}
	if c.config.UTSNsCtr != "" {
		if err := c.addNamespaceContainer(&g, UTSNS, c.config.UTSNsCtr, spec.UTSNamespace); err != nil {
			return nil, err
		}
	}
	if c.config.CgroupNsCtr != "" {
		if err := c.addNamespaceContainer(&g, CgroupNS, c.config.CgroupNsCtr, spec.CgroupNamespace); err != nil {
			return nil, err
		}
	}

	g.SetRootPath(c.state.Mountpoint)
	g.AddAnnotation(crioAnnotations.Created, c.config.CreatedTime.Format(time.RFC3339Nano))
	g.AddAnnotation("org.opencontainers.image.stopSignal", fmt.Sprintf("%d", c.config.StopSignal))

	g.SetHostname(c.Hostname())
	g.AddProcessEnv("HOSTNAME", g.Spec().Hostname)

	return g.Spec(), nil
}

// Add an existing container's namespace to the spec
func (c *Container) addNamespaceContainer(g *generate.Generator, ns LinuxNS, nsCtrID string, specNS string) error {
	nsCtr, err := c.runtime.state.Container(nsCtrID)
	if err != nil {
		return err
	}

	nsPath, err := nsCtr.NamespacePath(ns)
	if err != nil {
		return err
	}

	if err := g.AddOrReplaceLinuxNamespace(specNS, nsPath); err != nil {
		return err
	}

	return nil
}

func (c *Container) addImageVolumes(g *generate.Generator) error {
	mountPoint := c.state.Mountpoint
	if !c.state.Mounted {
		return errors.Wrapf(ErrInternal, "container is not mounted")
	}

	imageStorage, err := c.runtime.getImage(c.config.RootfsImageID)
	if err != nil {
		return err
	}
	imageData, err := c.runtime.getImageInspectInfo(*imageStorage)
	if err != nil {
		return err
	}

	for k := range imageData.ContainerConfig.Volumes {
		mount := spec.Mount{
			Destination: k,
			Type:        "bind",
			Options:     []string{"rbind", "rw"},
		}
		if MountExists(g.Mounts(), k) {
			continue
		}
		volumePath := filepath.Join(c.config.StaticDir, "volumes", k)
		if _, err := os.Stat(volumePath); os.IsNotExist(err) {
			if err = os.MkdirAll(volumePath, 0755); err != nil {
				return errors.Wrapf(err, "error creating directory %q for volume %q in container %q", volumePath, k, c.ID)
			}
			if err = label.Relabel(volumePath, c.config.MountLabel, false); err != nil {
				return errors.Wrapf(err, "error relabeling directory %q for volume %q in container %q", volumePath, k, c.ID)
			}
			srcPath := filepath.Join(mountPoint, k)
			if err = chrootarchive.NewArchiver(nil).CopyWithTar(srcPath, volumePath); err != nil && !os.IsNotExist(err) {
				return errors.Wrapf(err, "error populating directory %q for volume %q in container %q using contents of %q", volumePath, k, c.ID, srcPath)
			}
			mount.Source = volumePath
		}
		g.AddMount(mount)
	}
	return nil
}
