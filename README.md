# Custom Scheduler Controller

This repository contains the implementation of the custom scheduler controller component.
This component works with [the extended Knative Serving implementation](https://github.com/Tarik-Kada/knative-serving).
When deployedin a cluster that runs the extended Knative Serving, it will handle the logic for
the scheduling of pods running user workload.

The logic is found in the [customscheduler_controller.go](https://github.com/Tarik-Kada/custom-scheduler-controller/blob/main/internal/controller/customscheduler_controller.go) file. It handles:

- Finding of pods to schedule
- Reading of scheduler configuration, including
  - Scheduler name and namespace within the cluster
  - Reading custom metrics defined by the user
  - Reading the parameters defined by the user
- Executing custom metrics defined by the user
- Passing of custom metrics, parameters, and cluster info to the external scheduling algorithm
- Retrieving the scheduling decision and binding the pod accordingly.

The repository also contains [examples](https://github.com/Tarik-Kada/custom-scheduler-controller/blob/main/hack/templates) for the deployment of a custom scheduling algorithm that works with the controller logic to produce scheduling decisions.