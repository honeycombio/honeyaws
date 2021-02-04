#!/bin/sh

# Smoke-test package installation by installing packages into a container. This
# assumes that packages exist in $GOPATH/bin

set -e

if [ "$#" -ne 1 ]; then echo "usage: test.sh <build id>"; exit 1; fi

BUILDID=`echo $1 | sed 's/v//g'`
DEB=honeyaws_${BUILDID}_amd64.deb
RPM=honeyaws-${BUILDID}-1.x86_64.rpm
DIR=`dirname $0`
echo docker build --build-arg package=$DEB -f Dockerfile.deb $DIR
echo docker build --build-arg package=$RPM -f Dockerfile.deb $DIR

cp $GOPATH/bin/$DEB $DIR
cp $GOPATH/bin/$RPM $DIR
docker build --build-arg package=$DEB -f $DIR/Dockerfile.deb $DIR
docker build --build-arg package=$RPM -f $DIR/Dockerfile.rpm $DIR

echo finished