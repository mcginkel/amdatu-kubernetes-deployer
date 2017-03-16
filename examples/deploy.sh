#!/usr/bin/env bash

NAMESPACE="default"
DEPLOYER="http://localhost:8000"

set -e

echo "Run this in the examples directory!"

echo "Creating descriptor..."
result=`curl -s --dump-header - --data @descriptor.json "${DEPLOYER}/descriptors/?namespace=${NAMESPACE}"`

locationHeader=`echo -n "$result" | grep "Location"`
location="${locationHeader:10}"
descriptorId=`expr "$location" : '/descriptors/\(.*\)/?'`
echo "descriptor id: ${descriptorId}"

echo "Deploying..."
result=`curl -s --dump-header - -X POST "$DEPLOYER/deployments/?namespace=${NAMESPACE}&descriptorId=${descriptorId}"`
locationHeader=`echo -n "${result}" | grep "Location"`
location="${locationHeader:10}"
deploymentId=`expr "$location" : '/deployments/\(.*\)/?'`
echo "deployment id: ${deploymentId}"

echo "Checking deployment status..."
status="UNKNOWN"
while [ "${status}" != "DEPLOYED" -a "${status}" != "FAILURE" ]
do
    result=`curl -sS "${DEPLOYER}/deployments/${deploymentId}/?namespace=${NAMESPACE}"`
    statusLine=`echo "${result}" | grep "status"`
    status=`expr "$statusLine" : '.*: "\(.*\)"'`
    echo "Deployment status: ${status}"
    sleep 1
done

echo "Getting deployment logs"
curl -sS "${DEPLOYER}/deployments/${deploymentId}/logs?namespace=${NAMESPACE}"

echo "Getting healthcheck data"
curl -sS "${DEPLOYER}/deployments/${deploymentId}/healthcheckdata?namespace=${NAMESPACE}"
