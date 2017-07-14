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

Name:           $NAME
Version:        $VERSION
Release:        0
License:        Apache-2.0
Summary:        Automatically load all docker images that are packaged in RPM
Url:            https://%{import_path}
Group:          System/Management
Source:         ${SAFE_BRANCH}.tar.gz
Source1:        sysconfig.%{name}
Source2:        %{name}.service
BuildRequires:  golang-packaging systemd
BuildRoot:      %{_tmppath}/%{name}-%{version}-build
Requires:       docker
Conflicts:      docker > 1.12.6
Requires(post): %fillup_prereq

%{?systemd_requires}

%{go_nostrip}
%{go_provides}

%description
Find all docker images that are packaged in RPM and load all them in docker daemon.

%prep
%setup -q -n ${NAME}-${SAFE_BRANCH}

%build
%goprep %{import_path}
%gobuild .

%install
mkdir -p %{buildroot}/%{_localstatedir}/adm/fillup-templates/
install -D -m 0644 %{S:1} %{buildroot}/%{_localstatedir}/adm/fillup-templates/

mkdir -p %{buildroot}/%{_unitdir}
install -D -m 0644 %{S:2} %{buildroot}/%{_unitdir}/
mkdir -p %{buildroot}/%{_sbindir}
ln -s %{_sbindir}/service %{buildroot}/%{_sbindir}/rc%{name}

install -m 0755 ../go/bin/%{name} %{buildroot}/%{_bindir}

%pre
%service_add_pre %{name}.service

%post
%service_add_post %{name}.service
%fillup_only -n %{name}

%preun
%service_del_preun %{name}.service

%postun
%service_del_postun %{name}.service

%files
%defattr(-,root,root)
%doc README.md LICENSE
%{_bindir}/%{name}
%{_sbindir}/rc%{name}
%{_unitdir}/%{name}.service
%config %{_localstatedir}/adm/fillup-templates/sysconfig.%{name}


%changelog
EOF
