This is a simple program that looks for all the Docker images shipped by RPMs,
finds the ones that are not available to the Docker daemon running on the local
machine and imports the missing ones.

This is a temporary solution needed until SUSE hosts its own image repository.

# Building

Checkout the code and then run:

```
make build
```

This project uses [hellogopher](https://github.com/cloudflare/hellogopher) to
simplify the building aspects of it.

The `container-feeder` binary can be found under the `bin/` directory.

# Usage

To import all the missing RPM images:

```
./container-feeder
```

It's possible to specify the name of the directory containing all the `.tar.xz`
files:

```
./container-feeder --dir <path to dir>
```

By default the program will look for the images under `/usr/share/suse-docker-images/native`.

# Limitations

This program will *"docker load"* all the `.tar.xz` images that have to be
available to the local docker daemon.

These images must be native docker images, they cannot be tarballs containing
just the root filesystem of the image. Hence it's not possible to import the
pre-built Docker images that are currently shipped by SUSE.

# License

`container-feeder` is licensed under the terms of the Apache 2.0 license.

```
container-feeder: import Linux container images delivered as RPMs
Copyright 2017 SUSE LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```
