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

    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/log"
    "sigs.k8s.io/controller-runtime/pkg/handler"
    // "sigs.k8s.io/controller-runtime/pkg/source"
    "sigs.k8s.io/controller-runtime/pkg/event"
    "sigs.k8s.io/controller-runtime/pkg/predicate"
    "sigs.k8s.io/controller-runtime/pkg/reconcile"
    "sigs.k8s.io/controller-runtime/pkg/builder"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    ctrl "sigs.k8s.io/controller-runtime"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/types"

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
    Parameters      map[string]interface{} `json:"parameters"`
    Pod             FilteredPod       `json:"pod"`
    ClusterInfo     ClusterInfo      `json:"clusterInfo"`
    Metrics         map[string]interface{} `json:"metrics"`
    PrometheusError string         `json:"prometheusError,omitempty"`
}

type ClusterInfo struct {
    Nodes []NodeInfo `json:"nodes"`
}

type NodeInfo struct {
    NodeName          string   `json:"nodeName"`
    Status            string   `json:"nodeStatus"`
    CpuCapacity       string   `json:"cpuCapacity"`
    MemoryCapacity    string   `json:"memoryCapacity"`
    CpuAllocatable    string   `json:"cpuAllocatable"`
    MemoryAllocatable string   `json:"memoryAllocatable"`
    RunningPods       []FilteredPod `json:"runningPods"`
}

type FilteredPod struct {
    Name         string            `json:"name"`
    Namespace    string            `json:"namespace"`
    Labels       map[string]string `json:"labels"`
    ServingService string          `json:"servingService"`
    ServingRevision string         `json:"servingRevision"`
    Container    ContainerInfo     `json:"container"`
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


func (r *CustomSchedulerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    logger := log.FromContext(ctx)
    logger.Info("Reconcile function called", "name", req.Name, "namespace", req.Namespace)

    // Fetch the CustomScheduler instance
    var customScheduler servingv1alpha1.CustomScheduler
    if err := r.Get(ctx, req.NamespacedName, &customScheduler); err != nil {
        logger.Error(err, "Failed to get CustomScheduler")
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    logger.Info("Reconciling CustomScheduler", "schedulerName", customScheduler.Spec.SchedulerName)

    // Fetch all Pods with the specified schedulerName
    var pods corev1.PodList
    if err := r.List(ctx, &pods, client.MatchingFields{"spec.schedulerName": customScheduler.Spec.SchedulerName}); err != nil {
        return ctrl.Result{}, err
    }

    if len(pods.Items) != 0 {
        logger.Info("Found pods", "count", len(pods.Items))
    }
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
    if err := json.Unmarshal([]byte(configMap.Data["customMetrics"]), &customMetrics); err != nil {
        logger.Error(err, "Failed to unmarshal custom metrics")
        return ctrl.Result{}, err
    }

    var parameters map[string]interface{}
    if err := json.Unmarshal([]byte(configMap.Data["parameters"]), &parameters); err != nil {
        logger.Error(err, "Failed to unmarshal parameters")
        return ctrl.Result{}, err
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
    for _, pod := range pods.Items {
        if pod.Spec.NodeName == "" { // Check if the pod is not assigned to any node
            filteredPod := FilteredPod{
                Name:         pod.Name,
                Namespace:    pod.Namespace,
                Labels:       pod.Labels,
                ServingService: pod.Labels["serving.knative.dev/service"],
                ServingRevision: pod.Labels["serving.knative.dev/revision"],
                Container: ContainerInfo{
                    Name:  pod.Spec.Containers[0].Name, // Assuming the first container is the user-container
                    Image: pod.Spec.Containers[0].Image, // Assuming the first container is the user-container
                },
            }

            request := SchedulerRequest{
                Parameters:      parameters,
                Pod:             filteredPod,
                ClusterInfo:     clusterInfo,
                Metrics:         metrics,
                PrometheusError: prometheusError,
            }

            nodeName, err := r.getNodeFromScheduler(request)
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
        }
    }

    return ctrl.Result{}, nil
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
            CpuCapacity:       node.Status.Capacity.Cpu().String(),
            MemoryCapacity:    node.Status.Capacity.Memory().String(),
            CpuAllocatable:    node.Status.Allocatable.Cpu().String(),
            MemoryAllocatable: node.Status.Allocatable.Memory().String(),
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
        filteredPod := FilteredPod{
            Name:       pod.Name,
            Namespace:  pod.Namespace,
            Labels:     pod.Labels,
            ServingService: pod.Labels["serving.knative.dev/service"],
            ServingRevision: pod.Labels["serving.knative.dev/revision"],
            Container: ContainerInfo{
                Name:  pod.Spec.Containers[0].Name, // Assuming the first container is the user-container
                Image: pod.Spec.Containers[0].Image, // Assuming the first container is the user-container
            },
        }
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

func (r *CustomSchedulerReconciler) getNodeFromScheduler(request SchedulerRequest) (string, error) {
    client := &http.Client{}
    reqBody, err := json.Marshal(request)
    if err != nil {
        return "", err
    }

    req, err := http.NewRequest("POST", "http://scheduler-simple-flask.default.svc.cluster.local/schedule", bytes.NewBuffer(reqBody))
    if err != nil {
        return "", err
    }

    req.Header.Set("Content-Type", "application/json")

    resp, err := client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("unexpected status code: %v", resp.StatusCode)
    }

    var schedulerResponse SchedulerResponse
    if err := json.NewDecoder(resp.Body).Decode(&schedulerResponse); err != nil {
        return "", err
    }

    return schedulerResponse.Node, nil
}

// SetupWithManager sets up the controller with the Manager.
// func (r *CustomSchedulerReconciler) SetupWithManager(mgr ctrl.Manager) error {
//     if err := mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Pod{}, "spec.schedulerName", func(rawObj client.Object) []string {
//         pod := rawObj.(*corev1.Pod)
//         return []string{pod.Spec.SchedulerName}
//     }); err != nil {
//         return err
//     }

//     return ctrl.NewControllerManagedBy(mgr).
//         For(&servingv1alpha1.CustomScheduler{}).
//         Watches(
//             &corev1.Pod{},
//             &handler.EnqueueRequestForObject{},
//         ).
//         Complete(r)
// }

func podToCustomSchedulerMapper(a handler.MapFunc) []reconcile.Request {
    // Get the scheduler name from the Pod
    schedulerName := a.Meta.GetAnnotations()["scheduler.alpha.kubernetes.io/name"]

    // Return a reconcile request for the CustomScheduler with the same name
    return []reconcile.Request{
        {NamespacedName: types.NamespacedName{Name: schedulerName}},
    }
}

func (r *CustomSchedulerReconciler) SetupWithManager(mgr ctrl.Manager) error {
    if err := mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Pod{}, "spec.schedulerName", func(rawObj client.Object) []string {
        pod := rawObj.(*corev1.Pod)
        return []string{pod.Spec.SchedulerName}
    }); err != nil {
        return err
    }

    schedulerNamePredicate := predicate.Funcs{
        UpdateFunc: func(e event.UpdateEvent) bool {
            return e.ObjectNew.(*corev1.Pod).Spec.SchedulerName == "custom-scheduler"
        },
        CreateFunc: func(e event.CreateEvent) bool {
            return e.Object.(*corev1.Pod).Spec.SchedulerName == "custom-scheduler"
        },
    }

    return ctrl.NewControllerManagedBy(mgr).
        For(&servingv1alpha1.CustomScheduler{}).
        Watches(
            &corev1.Pod{},
            &handler.EnqueueRequestsFromMapFunc{
                ToRequests: handler.ToRequestsFunc(func(a client.Object) []reconcile.Request {
                    return podToCustomSchedulerMapper(a)
                }),
            },
            builder.WithPredicates(schedulerNamePredicate),
        ).
        Complete(r)
}