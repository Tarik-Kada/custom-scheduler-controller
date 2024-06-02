# Example Scheduler using Python Flask

## Dockerfile

The Dockerfile is used to assemble a working image for the custom scheduler.

## scheduler.py

The scheduler.py file contains the implementation of the custom scheduling algorithm

## config.yaml

The config.yaml file contains the ConfigMap of the scheduler deployment. This configuration
is used by both the [custom scheduler controller](https://github.com/Tarik-Kada/custom-scheduler-controller/blob/main/internal/controller/customscheduler_controller.go) and the [extended controller](https://github.com/Tarik-Kada/knative-serving).
The user can change the information that gets passed to the external scheduling algorithm through this config.
In addition to this, the user can change which external scheduling algorithm will be used to get the scheduling decisions.
This can be done whil the cluster is running. The cluster does not have to be restarted for the changes to take effect.

## deployment.yaml

The deployment.yaml defines the deployment of the external scheduling algorithm. It deploys to the cluster
as a container running in a pod. The file defines which port the scheduling algorithm is listening on, and thus
to which port the scheduling request should be sent.

## deploy.sh

The deploy.sh is a Bash script that can be used to quickly deploy the external scheduling algorithm to
the cluster to which the kubectl context is currently set. Make sure the kubectl context is correctly set.