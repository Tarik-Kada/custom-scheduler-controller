/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "bytes"
    "net/url"
    "strings"
    "time"

    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/log"
    "sigs.k8s.io/controller-runtime/pkg/handler"
    "sigs.k8s.io/controller-runtime/pkg/predicate"
    "sigs.k8s.io/controller-runtime/pkg/reconcile"
    "sigs.k8s.io/controller-runtime/pkg/builder"
    corev1 "k8s.io/api/core/v1"
    appsv1 "k8s.io/api/apps/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    ctrl "sigs.k8s.io/controller-runtime"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/types"

    "github.com/go-logr/logr"

    servingv1alpha1 "github.com/Tarik-Kada/custom-scheduler-controller/api/v1alpha1"
)

// CustomSchedulerReconciler reconciles a CustomScheduler object
type CustomSchedulerReconciler struct {
    client.Client
    Scheme *runtime.Scheme
}

type SchedulerResponse struct {
    Node string `json:"node"`
}

type CustomMetric struct {
    MetricName string `json:"metricName"`
    Query      string `json:"query"`
}

type SchedulerRequest struct {
    Parameters      map[string]interface{}  `json:"parameters"`
    Pod             FilteredPod             `json:"pod"`
    ClusterInfo     ClusterInfo             `json:"clusterInfo"`
    Metrics         map[string]interface{}  `json:"metrics"`
    PrometheusError string                  `json:"prometheusError,omitempty"`
}

type ClusterInfo struct {
    Nodes []NodeInfo `json:"nodes"`
}

type NodeInfo struct {
    NodeName          string              `json:"nodeName"`
    Status            string              `json:"nodeStatus"`
    CpuCapacity       int64               `json:"cpuCapacity"`
    MemoryCapacity    int64               `json:"memoryCapacity"`
    EphemeralCapacity int64               `json:"ephemeralCapacity"`
    CpuUsage          int64               `json:"cpuUsage"`
    MemoryUsage       int64               `json:"memoryUsage"`
    EphemeralUsage    int64               `json:"ephemeralUsage"`
    ScalarResources   map[string]int64    `json:"scalarResources"`
    RunningPods       []FilteredPod       `json:"runningPods"`
}

type FilteredPod struct {
    Name             string            `json:"name"`
    Namespace        string            `json:"namespace"`
    Labels           map[string]string `json:"labels"`
    CpuRequests      int64             `json:"cpuRequests"`
    MemoryRequests   int64             `json:"memoryRequests"`
    EphemeralRequests int64            `json:"ephemeralRequests"`
    ScalarRequests   map[string]int64  `json:"scalarRequests"`
    Containers       []ContainerInfo   `json:"containers"`
}

type ContainerInfo struct {
    Name  string `json:"name"`
    Image string `json:"image"`
}

//+kubebuilder:rbac:groups=serving.local.dev,resources=customschedulers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=serving.local.dev,resources=customschedulers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=serving.local.dev,resources=customschedulers/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=pods;pods/binding;bindings,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch


func (r *CustomSchedulerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    logger := log.FromContext(ctx)
    logger.Info("Reconcile function called", "name", req.Name, "namespace", req.Namespace)

    logger.Info("Trying to get pod: ", "name", req.Name, "namespace", req.Namespace)
    // Get the pod that triggered the reconcile
    var pod corev1.Pod
    if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
        logger.Info("Failed to get Pod, Probably not workload pod")
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // List all CustomScheduler instances
    var customSchedulers servingv1alpha1.CustomSchedulerList
    if err := r.List(ctx, &customSchedulers, &client.ListOptions{Namespace: "default"}); err != nil {
        logger.Error(err, "Failed to list CustomSchedulers")
        return ctrl.Result{}, err
    }

    if len(customSchedulers.Items) == 0 {
        logger.Error(fmt.Errorf("no CustomScheduler found"), "No CustomScheduler instances found")
        return ctrl.Result{}, nil
    }

    // Assuming there's only one CustomScheduler instance
    customScheduler := customSchedulers.Items[0]
    logger.Info("Reconciling CustomScheduler", "Name", customScheduler.Name, "schedulerName", customScheduler.Spec.SchedulerName)

    // Get cluster information
    clusterInfo, err := r.getClusterInfo(ctx)
    if err != nil {
        logger.Error(err, "Failed to get cluster information")
        return ctrl.Result{}, err
    }

    // Load custom metrics from ConfigMap
    var configMap corev1.ConfigMap
    if err := r.Get(ctx, types.NamespacedName{Name: "scheduler-config", Namespace: "default"}, &configMap); err != nil {
        logger.Error(err, "Failed to get scheduler-config ConfigMap")
        return ctrl.Result{}, err
    }

    var customMetrics []CustomMetric
    if data, exists := configMap.Data["customMetrics"]; exists && data != "" {
        logger.Info("Custom metrics found in ConfigMap", "data", data)
        if err := json.Unmarshal([]byte(data), &customMetrics); err != nil {
            logger.Error(err, "Failed to unmarshal custom metrics")
            return ctrl.Result{}, err
        }
    }

    var parameters map[string]interface{}
    if data, exists := configMap.Data["parameters"]; exists && data != "" {
        if err := json.Unmarshal([]byte(data), &parameters); err != nil {
            logger.Error(err, "Failed to unmarshal parameters")
            return ctrl.Result{}, err
        }
    }

    schedulerName := configMap.Data["schedulerName"]
    schedulerNamespace := configMap.Data["schedulerNamespace"]

    if schedulerName == "" || schedulerNamespace == "" {
        logger.Error(nil, "Scheduler name or namespace is not defined in the scheduler-config ConfigMap")
        return ctrl.Result{}, fmt.Errorf("scheduler name or namespace is not defined")
    }
    logger.Info("Scheduler name and namespace", "schedulerName", schedulerName, "schedulerNamespace", schedulerNamespace)

    // Read the Deployment and Service for the custom scheduler
    var schedulerService corev1.Service
    if err := r.Get(ctx, types.NamespacedName{Name: schedulerName, Namespace: schedulerNamespace}, &schedulerService); err != nil {
        logger.Error(err, "Failed to get Scheduler Service")
        return ctrl.Result{}, err
    }

    var schedulerDeployment appsv1.Deployment
    if err := r.Get(ctx, types.NamespacedName{Name: schedulerName, Namespace: schedulerNamespace}, &schedulerDeployment); err != nil {
        logger.Error(err, "Failed to get Scheduler Deployment")
        return ctrl.Result{}, err
    }

    // Construct the URL for the scheduler
    schedulerURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d",
                                schedulerService.Name,
                                schedulerService.Namespace,
                                schedulerService.Spec.Ports[0].Port)

    logger.Info("Scheduler URL", "url", schedulerURL)

    // Fetch all Pods with the specified schedulerName
    var pods corev1.PodList
    if err := r.List(ctx, &pods, client.MatchingFields{"spec.schedulerName": customScheduler.Spec.SchedulerName}); err != nil {
        return ctrl.Result{}, err
    }

    if len(pods.Items) != 0 {
        logger.Info("Found pods", "count", len(pods.Items))
    }

    prometheusURL := "http://prometheus-kube-prometheus-prometheus.default.svc.cluster.local:9090"
    metrics := make(map[string]interface{})
    prometheusError := ""

    if isPrometheusAvailable(prometheusURL) {
        metrics, err = r.getCustomMetrics(prometheusURL, customMetrics)
        if err != nil {
            prometheusError = "Prometheus is available but failed to retrieve metrics: " + err.Error()
            logger.Error(err, prometheusError)
        }
    } else {
        prometheusError = "Prometheus is unavailable!"
        logger.Info("Prometheus is unavailable", "error", prometheusError)

    }

    // Bind each unassigned Pod to a node specified by the external scheduler
    if pod.Spec.NodeName == "" { // Check if the pod is not assigned to any node
        filteredPod := createFilteredPod(&pod)

        request := SchedulerRequest{
            Parameters:      parameters,
            Pod:             filteredPod,
            ClusterInfo:     clusterInfo,
            Metrics:         metrics,
            PrometheusError: prometheusError,
        }

        nodeName, err := r.getNodeFromScheduler(logger, request, schedulerURL)
        if err != nil {
            logger.Error(err, "Failed to get node from scheduler")
            return ctrl.Result{}, err
        }

        logger.Info("Binding pod", "podName", pod.Name, "nodeName", nodeName)
        binding := &corev1.Binding{
            ObjectMeta: metav1.ObjectMeta{
                Name:      pod.Name,
                Namespace: pod.Namespace,
            },
            Target: corev1.ObjectReference{
                Kind: "Node",
                Name: nodeName,
            },
        }
        if err := r.Client.Create(ctx, binding); err != nil {
            logger.Error(err, "Failed to bind pod", "podName", pod.Name)
            return ctrl.Result{}, err
        }

        r.createScheduledEvent(ctx, &pod, nodeName)
    }

    return ctrl.Result{}, nil
}

func (r *CustomSchedulerReconciler) createScheduledEvent(ctx context.Context, pod *corev1.Pod, nodeName string) {
    event := &corev1.Event{
        ObjectMeta: metav1.ObjectMeta{
            Name:      fmt.Sprintf("%s.%x", pod.Name, time.Now().UnixNano()),
            Namespace: pod.Namespace,
        },
        InvolvedObject: corev1.ObjectReference{
            Kind:      "Pod",
            Namespace: pod.Namespace,
            Name:      pod.Name,
            UID:       pod.UID,
        },
        Reason:  "Scheduled",
        Message: fmt.Sprintf("Successfully assigned %s/%s to %s", pod.Namespace, pod.Name, nodeName),
        Source: corev1.EventSource{
            Component: "custom-scheduler",
        },
        FirstTimestamp: metav1.Now(),
        LastTimestamp:  metav1.Now(),
        Count:          1,
        Type:           corev1.EventTypeNormal,
    }

    if err := r.Client.Create(ctx, event); err != nil {
        log.FromContext(ctx).Error(err, "Failed to create scheduled event")
    }
}

func isPrometheusAvailable(prometheusURL string) bool {
    resp, err := http.Get(prometheusURL + "/api/v1/query?query=up")
    if err != nil || resp.StatusCode != http.StatusOK {
        return false
    }
    return true
}

func (r *CustomSchedulerReconciler) getCustomMetrics(prometheusURL string, queries []CustomMetric) (map[string]interface{}, error) {
    metrics := make(map[string]interface{})

    for _, metric := range queries {
        resp, err := http.Get(prometheusURL + "/api/v1/query?query=" + url.QueryEscape(metric.Query))
        if err != nil {
            return nil, err
        }
        defer resp.Body.Close()

        var result map[string]interface{}
        if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
            return nil, err
        }
        metrics[metric.MetricName] = result["data"]
    }
    return metrics, nil
}

func createFilteredPod(pod *corev1.Pod) FilteredPod {
    cpuRequests, memoryRequests, ephemeralRequests := int64(0), int64(0), int64(0)
    scalarRequests := make(map[string]int64)

    for _, container := range pod.Spec.Containers {
        cpuRequests += container.Resources.Requests.Cpu().MilliValue()
        memoryRequests += container.Resources.Requests.Memory().Value()
        ephemeralRequests += container.Resources.Requests.StorageEphemeral().Value()
        for name, quantity := range container.Resources.Requests {
            if strings.HasPrefix(string(name), "scalar/") {
                scalarRequests[string(name)] += quantity.Value()
            }
        }
    }

    containers := make([]ContainerInfo, len(pod.Spec.Containers))
    for i, container := range pod.Spec.Containers {
        containers[i] = ContainerInfo{
            Name:  container.Name,
            Image: container.Image,
        }
    }

    return FilteredPod{
        Name:              pod.Name,
        Namespace:         pod.Namespace,
        Labels:            pod.Labels,
        CpuRequests:       cpuRequests,
        MemoryRequests:    memoryRequests,
        EphemeralRequests: ephemeralRequests,
        ScalarRequests:    scalarRequests,
        Containers:        containers,
    }
}

func (r *CustomSchedulerReconciler) getNodeInfo(ctx context.Context) ([]NodeInfo, error) {
    var nodes corev1.NodeList
    if err := r.List(ctx, &nodes); err != nil {
        return nil, err
    }

    var nodeInfos []NodeInfo
    for _, node := range nodes.Items {
        // Filter out nodes that have the control-plane role
        if _, ok := node.Labels["node-role.kubernetes.io/control-plane"]; ok {
            continue
        }

        nodeInfo := NodeInfo{
            NodeName:          node.Name,
            Status:            getNodeStatus(&node),
            CpuCapacity:       node.Status.Capacity.Cpu().MilliValue(),
            MemoryCapacity:    node.Status.Capacity.Memory().Value(),
            EphemeralCapacity: node.Status.Capacity.StorageEphemeral().Value(),
            CpuUsage:          node.Status.Allocatable.Cpu().MilliValue() - node.Status.Capacity.Cpu().MilliValue(),
            MemoryUsage:       node.Status.Allocatable.Memory().Value() - node.Status.Capacity.Memory().Value(),
            EphemeralUsage:    node.Status.Allocatable.StorageEphemeral().Value() - node.Status.Capacity.StorageEphemeral().Value(),
            ScalarResources:   make(map[string]int64),
            RunningPods:       []FilteredPod{},
        }

        nodeInfos = append(nodeInfos, nodeInfo)
    }
    return nodeInfos, nil
}

func getNodeStatus(node *corev1.Node) string {
    for _, condition := range node.Status.Conditions {
        if condition.Type == corev1.NodeReady {
            if condition.Status == corev1.ConditionTrue {
                return "Ready"
            }
            return "NotReady"
        }
    }
    return "Unknown"
}

func (r *CustomSchedulerReconciler) getPodInfo(ctx context.Context) (map[string][]FilteredPod, error) {
    var pods corev1.PodList
    if err := r.List(ctx, &pods); err != nil {
        return nil, err
    }

    filteredPodMap := make(map[string][]FilteredPod)
    for _, pod := range pods.Items {
        filteredPod := createFilteredPod(&pod)
        filteredPodMap[pod.Spec.NodeName] = append(filteredPodMap[pod.Spec.NodeName], filteredPod)
    }
    return filteredPodMap, nil
}

func (r *CustomSchedulerReconciler) getClusterInfo(ctx context.Context) (ClusterInfo, error) {
    nodeInfos, err := r.getNodeInfo(ctx)
    if err != nil {
        return ClusterInfo{}, err
    }

    podInfoMap, err := r.getPodInfo(ctx)
    if err != nil {
        return ClusterInfo{}, err
    }

    for i, nodeInfo := range nodeInfos {
        if pods, exists := podInfoMap[nodeInfo.NodeName]; exists {
            nodeInfos[i].RunningPods = pods
        }
    }

    return ClusterInfo{Nodes: nodeInfos}, nil
}

func (r *CustomSchedulerReconciler) getNodeFromScheduler(logger logr.Logger, request SchedulerRequest, schedulerURL string) (string, error) {
    client := &http.Client{}
    reqBody, err := json.Marshal(request)

    if err != nil {
        logger.Error(err, "Error marshalling request")
        return "", err
    }
    logger.Info("Request marshalled successfully")

    logger.Info("Creating new HTTP request", "url", schedulerURL)
    req, err := http.NewRequest("POST", schedulerURL, bytes.NewBuffer(reqBody))
    if err != nil {
        logger.Error(err, "Error creating new HTTP request")
        return "", err
    }
    req.Header.Set("Content-Type", "application/json")
    logger.Info("HTTP request created successfully")

    logger.Info("Sending HTTP request")
    resp, err := client.Do(req)
    if err != nil {
        logger.Error(err, "Error sending HTTP request")
        return "", err
    }
    logger.Info("HTTP request sent successfully")

    defer func() {
        logger.Info("Closing response body")
        resp.Body.Close()
    }()

    logger.Info("Received HTTP response", "status code", resp.StatusCode)
    if resp.StatusCode != http.StatusOK {
        logger.Error(fmt.Errorf("unexpected status code: %v", resp.StatusCode), "Unexpected status code")
        return "", fmt.Errorf("unexpected status code: %v", resp.StatusCode)
    }

    var schedulerResponse SchedulerResponse
    logger.Info("Decoding HTTP response body")
    if err := json.NewDecoder(resp.Body).Decode(&schedulerResponse); err != nil {
        logger.Error(err, "Error decoding HTTP response")
        return "", err
    }
    logger.Info("HTTP response body decoded successfully")

    logger.Info("Scheduler selected node", "node", schedulerResponse.Node)
    return schedulerResponse.Node, nil
}

// // SetupWithManager sets up the controller with the Manager.
func (r *CustomSchedulerReconciler) SetupWithManager(mgr ctrl.Manager) error {
    if err := mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Pod{}, "spec.schedulerName", func(rawObj client.Object) []string {
        pod := rawObj.(*corev1.Pod)
        return []string{pod.Spec.SchedulerName}
    }); err != nil {
        return err
    }

    return ctrl.NewControllerManagedBy(mgr).
        For(&servingv1alpha1.CustomScheduler{}).
        Watches(
            &corev1.Pod{},
            handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
                pod := a.(*corev1.Pod)
                return []reconcile.Request{
                    {NamespacedName: types.NamespacedName{
                        Name:      pod.Name,
                        Namespace: pod.Namespace,
                    }},
                }
            }),
            builder.WithPredicates(
                predicate.NewPredicateFuncs(func(obj client.Object) bool {
                    pod := obj.(*corev1.Pod)
                    return pod.Spec.SchedulerName == "custom-scheduler"
                }),
            ),
        ).
        Complete(r)
}
