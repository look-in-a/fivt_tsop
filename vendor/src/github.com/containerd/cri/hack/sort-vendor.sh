#!/bin/bash

# Copyright 2017 The Kubernetes Authors.
# Copyright 2018 The containerd Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

source $(dirname "${BASH_SOURCE[0]}")/utils.sh
cd ${ROOT}

echo "Sort vendor.conf..."
tmpdir="$(mktemp -d)"
trap "rm -rf ${tmpdir}" EXIT

awk -v RS= '{print > "'${tmpdir}/'TMP."NR}' vendor.conf
for file in ${tmpdir}/*; do
  if [[ -e "${tmpdir}/vendor.conf" ]]; then
    echo >> "${tmpdir}/vendor.conf"
  fi
  sort -Vru "${file}" >> "${tmpdir}/vendor.conf"
done

mv "${tmpdir}/vendor.conf" vendor.conf

echo "Please commit the change made by this file..."
