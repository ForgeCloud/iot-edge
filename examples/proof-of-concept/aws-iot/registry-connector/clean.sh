#!/usr/bin/env bash

#
# Copyright 2020 ForgeRock AS
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

AWS_REGISTRY_MANAGEMENT_USER="iot-registry-management-user"
AWS_THING_NAME="f971a95b-2fc6-4ce2-aed6-84f8c6cf6b05"


echo "Detach the management user policy"
aws iam detach-user-policy \
  --user-name ${AWS_REGISTRY_MANAGEMENT_USER} \
  --policy-arn arn:aws:iam::aws:policy/AWSIoTFullAccess

echo "Delete the management user [$AWS_REGISTRY_MANAGEMENT_USER]"
aws iam delete-user \
  --user-name ${AWS_REGISTRY_MANAGEMENT_USER}

echo "Delete IoT thing [$AWS_THING_NAME]"
aws iot delete-thing \
    --thing-name ${AWS_THING_NAME}
