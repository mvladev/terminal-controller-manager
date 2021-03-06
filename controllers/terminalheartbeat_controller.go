/*

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

package controllers

import (
	"context"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/controller"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	extensionsv1alpha1 "github.com/gardener/terminal-controller-manager/api/v1alpha1"
)

// TerminalHeartbeatReconciler reconciles a TerminalHeartbeat object
type TerminalHeartbeatReconciler struct {
	client.Client
	Log      logr.Logger
	Recorder record.EventRecorder
	Config   *extensionsv1alpha1.ControllerManagerConfiguration
}

func (r *TerminalHeartbeatReconciler) SetupWithManager(mgr ctrl.Manager, config extensionsv1alpha1.TerminalHeartbeatControllerConfiguration) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&extensionsv1alpha1.Terminal{}).
		Named("heartbeat").
		WithOptions(controller.Options{
			MaxConcurrentReconciles: config.MaxConcurrentReconciles,
		}).
		Complete(r)
}

func (r *TerminalHeartbeatReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()

	// Fetch the TerminalHeartbeat instance
	t := &extensionsv1alpha1.Terminal{}

	err := r.Get(ctx, req.NamespacedName, t)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	if !t.ObjectMeta.DeletionTimestamp.IsZero() {
		// ignore already deleted terminal resource
		return ctrl.Result{}, nil
	}

	lastHeartbeat := t.ObjectMeta.Annotations[extensionsv1alpha1.TerminalLastHeartbeat]
	if len(lastHeartbeat) == 0 {
		// if there is no heartbeat set, delete right away
		return ctrl.Result{}, r.deleteTerminal(ctx, t)
	}

	lastHeartBeatParsed, err := time.Parse(time.RFC3339, lastHeartbeat)
	if err != nil {
		return ctrl.Result{}, r.deleteTerminal(ctx, t)
	}

	ttl := r.Config.Controllers.TerminalHeartbeat.TimeToLive.Duration
	expiration := lastHeartBeatParsed.Add(ttl)

	expiresIn := expiration.Sub(time.Now().UTC())
	if expiresIn <= 0 {
		return ctrl.Result{}, r.deleteTerminal(ctx, t)
	}

	return ctrl.Result{RequeueAfter: expiresIn}, nil
}

func (r *TerminalHeartbeatReconciler) deleteTerminal(ctx context.Context, t *extensionsv1alpha1.Terminal) error {
	r.recordEventAndLog(t, corev1.EventTypeNormal, extensionsv1alpha1.EventDeleting, "Deleting terminal resource due to missing heartbeat")

	deleteCtx, cancelFunc := context.WithTimeout(ctx, time.Duration(30*time.Second))
	defer cancelFunc()

	if err := r.Delete(deleteCtx, t); err != nil {
		return err
	}

	r.recordEventAndLog(t, corev1.EventTypeNormal, extensionsv1alpha1.EventDeleted, "Deleted terminal resource")

	return nil
}

func (r *TerminalHeartbeatReconciler) recordEventAndLog(t *extensionsv1alpha1.Terminal, eventType, reason, messageFmt string, args ...interface{}) {
	r.Recorder.Eventf(t, eventType, reason, messageFmt, args)
	r.Log.Info(fmt.Sprintf(messageFmt, args...), "namespace", t.Namespace, "name", t.Name)
}
