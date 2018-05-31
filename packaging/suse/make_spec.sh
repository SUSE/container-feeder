#!/bin/bash

if [ -z "$1" ]; then
  cat <<EOF
usage:
  ./make_spec.sh PACKAGE [BRANCH]
EOF
  exit 1
fi

cd $(dirname $0)

YEAR=$(date +%Y)
VERSION=$(cat ../../VERSION)
REVISION=$(git rev-list HEAD | wc -l)
COMMIT=$(git rev-parse --short HEAD)
COMMIT_UNIX_TIME=$(git show -s --format=%ct)
VERSION="${VERSION%+*}+$(date -d @$COMMIT_UNIX_TIME +%Y%m%d).git_r${REVISION}_${COMMIT}"
NAME=$1
BRANCH=${2:-master}
SAFE_BRANCH=${BRANCH//\//-}

cat <<EOF > ${NAME}.spec
#
# spec file for package $NAME
#
# Copyright (c) $YEAR SUSE LINUX Products GmbH, Nuernberg, Germany.
#
# All modifications and additions to the file contributed by third parties
# remain the property of their copyright owners, unless otherwise agreed
# upon. The license for this file, and modifications and additions to the
# file, is the same license as for the pristine package itself (unless the
# license for the pristine package is not an Open Source License, in which
# case the license is the MIT License). An "Open Source License" is a
# license that conforms to the Open Source Definition (Version 1.9)
# published by the Open Source Initiative.

# Please submit bugfixes or comments via http://bugs.opensuse.org/
#

%global provider        github
%global provider_tld    com
%global project         kubic-project
%global repo            container-feeder
%global provider_prefix %{provider}.%{provider_tld}/%{project}/%{repo}
%global import_path     %{provider_prefix}

#Compat macro for new _fillupdir macro introduced in Nov 2017
%if ! %{defined _fillupdir}
  %define _fillupdir /var/adm/fillup-templates
%endif

Name:           $NAME
Version:        $VERSION
Release:        0
License:        Apache-2.0
Summary:        Load container images from RPMs into different container engines
Url:            https://github.com/kubic-project/container-feeder
Group:          System/Management
Source:         ${SAFE_BRANCH}.tar.gz
Source1:        sysconfig.%{name}
Source2:        %{name}.service
Source3:        %{name}-rpmlintrc
BuildRoot:      %{_tmppath}/%{name}-%{version}-build
BuildRequires:  device-mapper-devel
BuildRequires:  fdupes
BuildRequires:  glib2-devel-static
BuildRequires:  glibc-devel-static
BuildRequires:  go-go-md2man
BuildRequires:  golang-packaging
BuildRequires:  libapparmor-devel
BuildRequires:  libassuan-devel
BuildRequires:  libbtrfs-devel
BuildRequires:  libgpgme-devel
BuildRequires:  libseccomp-devel
BuildRequires:  golang(API) >= 1.7
Requires:       docker-kubic
Requires:       libcontainers-common
Requires:       libcontainers-image
Requires:       libcontainers-storage
Requires:       xz
Requires(post): %fillup_prereq
%{?systemd_requires}
%{go_nostrip}
%{go_provides}

%description
Load container images in the Docker archive format, installed by RPMs, into
different container engines, such as docker or crio (containers/storage).

%prep
%setup -q -n ${NAME}-${SAFE_BRANCH}

%build
export GOPATH=\$HOME/go
mkdir -p \$HOME/go/src/%{import_path}
rm -rf \$HOME/go/src/%{import_path}/*
cp -ar * \$HOME/go/src/%{import_path}
cd \$HOME/go/src/%{import_path}

go build -tags "containers_image_ostree_stub seccomp apparmor" \
         -o bin/container-feeder \
         main.go

%pre
%service_add_pre %{name}.service

%post
%service_add_post %{name}.service
%fillup_only -n %{name}

%preun
%service_del_preun %{name}.service

%postun
%service_del_postun %{name}.service

%install
cd \$HOME/go/src/%{import_path}
install -D -m 0755 bin/%{name} %{buildroot}/%{_bindir}/%{name}
install -D -m 0644 container-feeder.json %{buildroot}/%{_sysconfdir}/container-feeder.json

mkdir -p %{buildroot}/%{_fillupdir}
install -D -m 0644 %{SOURCE1} %{buildroot}/%{_fillupdir}

mkdir -p %{buildroot}/%{_unitdir}
install -D -m 0644 %{SOURCE2} %{buildroot}/%{_unitdir}/
mkdir -p %{buildroot}/%{_sbindir}
ln -s %{_sbindir}/service %{buildroot}/%{_sbindir}/rc%{name}

%fdupes %{buildroot}/%{_prefix}

%files
%doc README.md
%if 0%{?suse_version} < 1500
%doc LICENSE
%else
%license LICENSE
%endif
%{_bindir}/%{name}
%{_sbindir}/rc%{name}
%{_unitdir}/%{name}.service
%{_fillupdir}/sysconfig.%{name}
%config(noreplace) %{_sysconfdir}/container-feeder.json

%changelog
EOF
