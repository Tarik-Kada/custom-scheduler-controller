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
    "time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	servingv1alpha1 "github.com/Tarik-Kada/custom-scheduler-controller/api/v1alpha1"
)

// CustomSchedulerReconciler reconciles a CustomScheduler object
type CustomSchedulerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=serving.local.dev,resources=customschedulers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=serving.local.dev,resources=customschedulers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=serving.local.dev,resources=customschedulers/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=pods;pods/binding;bindings,verbs=get;list;watch;create;update;patch

func (r *CustomSchedulerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the CustomScheduler instance
    var customScheduler servingv1alpha1.CustomScheduler
    if err := r.Get(ctx, req.NamespacedName, &customScheduler); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

	logger.Info("Reconciling CustomScheduler", "schedulerName", customScheduler.Spec.SchedulerName)

    // Fetch all Pods with the specified schedulerName
    var pods corev1.PodList
    if err := r.List(ctx, &pods, client.MatchingFields{"spec.schedulerName": customScheduler.Spec.SchedulerName}); err != nil {
        return ctrl.Result{}, err
    }

    logger.Info("Found pods", "count", len(pods.Items))

    // Example: Bind each Pod to a hard-coded node "knative-worker"
    for _, pod := range pods.Items {
        if pod.Spec.NodeName == "" { // Check if the pod is not assigned to any node
            logger.Info("Binding pod", "podName", pod.Name)
            binding := &corev1.Binding{
                ObjectMeta: metav1.ObjectMeta{
                    Name:      pod.Name,
                    Namespace: pod.Namespace,
                },
                Target: corev1.ObjectReference{
                    Kind: "Node",
                    Name: "knative-worker",
                },
            }
            if err := r.Client.Create(ctx, binding); err != nil {
                logger.Error(err, "Failed to bind pod", "podName", pod.Name)
                return ctrl.Result{}, err
            }
        }
    }

    return ctrl.Result{RequeueAfter: time.Second * 5}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CustomSchedulerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Pod{}, "spec.schedulerName", func(rawObj client.Object) []string {
        pod := rawObj.(*corev1.Pod)
        return []string{pod.Spec.SchedulerName}
    }); err != nil {
        return err
    }
    return ctrl.NewControllerManagedBy(mgr).
        For(&servingv1alpha1.CustomScheduler{}).
        Complete(r)
}
