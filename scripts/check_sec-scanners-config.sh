#!/usr/bin/env bash

# This script checks thate the RC-Tag and the eventing-manager image have the tag of the corresponding release.

# Error handling:
set -o nounset  # treat unset variables as an error and exit immediately.
set -o errexit  # exit immediately when a command fails.
set -E          # needs to be set if we want the ERR trap
set -o pipefail # prevents errors in a pipeline from being masked

# Get release version
DESIRED_TAG="${1:-"main"}"

# Get eventing-manager tag from sec-scanners-config.yaml
SEC_SCAN_TO_CHECK="${2:-europe-docker.pkg.dev/kyma-project/prod/eventing-manager}"
IMAGE_TAG=$(cat sec-scanners-config.yaml | grep "${SEC_SCAN_TO_CHECK}" | cut -d : -f 2)

# Get rc-tag
RC_TAG_TO_CHECK="${3:-rc-tag}"
RC_TAG=$(cat sec-scanners-config.yaml | grep "${RC_TAG_TO_CHECK}" | cut -d : -f 2 | xargs)

# Check IMAGE_TAG and required image tag
if [[ "$IMAGE_TAG" != "$DESIRED_TAG" ]] || [[ "$RC_TAG" != "$DESIRED_TAG" ]]; then
	# ERROR: Tag issue
	echo "Tags are not correct:
  - wanted: $DESIRED_TAG
  - security-scanner image tag: $IMAGE_TAG
  - rc-tag: $RC_TAG"
	exit 1
fi

# OK: Everything is fine
echo "Tags are correct"
exit 0
