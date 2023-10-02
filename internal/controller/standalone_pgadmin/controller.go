// Copyright 2023 Crunchy Data Solutions, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package standalone_pgadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/crunchydata/postgres-operator/internal/initialize"
	"github.com/crunchydata/postgres-operator/internal/logging"
	"github.com/crunchydata/postgres-operator/internal/naming"
	"github.com/crunchydata/postgres-operator/internal/util"
	"github.com/crunchydata/postgres-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// PGAdminReconciler reconciles a PGAdmin object
type PGAdminReconciler struct {
	client.Client
	Owner  client.FieldOwner
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=pgadmins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=pgadmins/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=pgadmins/finalizers,verbs=update

// Reconcile which aims to move the current state of the pgAdmin closer to the
// desired state described in a [v1beta1.PGAdmin] identified by request.
func (r *PGAdminReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	log := logging.FromContext(ctx)

	result := ctrl.Result{}

	pgAdmin := &v1beta1.PGAdmin{}
	if err := r.Get(ctx, req.NamespacedName, pgAdmin); err != nil {
		if err = client.IgnoreNotFound(err); err != nil {
			log.Error(err, "unable to fetch PGAdmin")
		}
		return result, err
	}
	log.Info("Reconciling pgAdmin")

	// Set defaults if unset
	pgAdmin.Default()

	_, err := r.reconcilePGAdminSecret(ctx, pgAdmin)

	if err == nil {
		clusters := r.getClustersForPGAdmin(ctx, pgAdmin)
		// clusters, err := r.getClustersForPGAdmin(ctx, pgAdmin)
		err = r.reconcilePGAdminConfigMap(ctx, pgAdmin, clusters)
	}
	// mount secret & configmap to deployment here

	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *PGAdminReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.PGAdmin{}).
		Watches(
			&source.Kind{Type: v1beta1.NewPostgresCluster()},
			r.watchPostgresClusters(),
		).
		Complete(r)
}

// watchPostgresClusters returns a [handler.EventHandler] for PostgresClusters.
func (r *PGAdminReconciler) watchPostgresClusters() handler.Funcs {
	handle := func(cluster client.Object, q workqueue.RateLimitingInterface) {
		ctx := context.Background()
		for _, pgadmin := range r.findPGAdminsForPostgresCluster(ctx, cluster) {

			q.Add(ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(pgadmin),
			})
		}
	}

	return handler.Funcs{
		CreateFunc: func(e event.CreateEvent, q workqueue.RateLimitingInterface) {
			handle(e.Object, q)
		},
		UpdateFunc: func(e event.UpdateEvent, q workqueue.RateLimitingInterface) {
			handle(e.ObjectNew, q)
		},
		DeleteFunc: func(e event.DeleteEvent, q workqueue.RateLimitingInterface) {
			handle(e.Object, q)
		},
	}
}

//+kubebuilder:rbac:groups="postgres-operator.crunchydata.com",resources="pgpgadmins",verbs={list}

// findPGAdminsForPostgresCluster returns PGAdmins that target cluster.
func (r *PGAdminReconciler) findPGAdminsForPostgresCluster(
	ctx context.Context, cluster client.Object,
) []*v1beta1.PGAdmin {
	var (
		matching []*v1beta1.PGAdmin
		pgadmins v1beta1.PGAdminList
	)

	// NOTE: If this becomes slow due to a large number of pgadmins in a single
	// namespace, we can configure the [ctrl.Manager] field indexer and pass a
	// [fields.Selector] here.
	// - https://book.kubebuilder.io/reference/watching-resources/externally-managed.html
	if r.List(ctx, &pgadmins, &client.ListOptions{
		Namespace: cluster.GetNamespace(),
	}) == nil {
		for i := range pgadmins.Items {
			for _, serverGroup := range pgadmins.Items[i].Spec.ServerGroups {
				if selector, err := naming.AsSelector(serverGroup.PostgresClusterSelector); err == nil {

					if selector.Matches(labels.Set(cluster.GetLabels())) {

						matching = append(matching, &pgadmins.Items[i])
					}
				}
			}
		}
	}
	return matching
}

// getClustersForPGAdmin returns clusters managed by the given pgAdmin
func (r *PGAdminReconciler) getClustersForPGAdmin(
	ctx context.Context,
	pgAdmin *v1beta1.PGAdmin,
) []*v1beta1.PostgresCluster {
	var matching []*v1beta1.PostgresCluster

	for _, serverGroup := range pgAdmin.Spec.ServerGroups {
		if selector, err := naming.AsSelector(serverGroup.PostgresClusterSelector); err == nil {

			var filteredList v1beta1.PostgresClusterList
			err = r.List(ctx, &filteredList,
				client.InNamespace(pgAdmin.Namespace),
				client.MatchingLabelsSelector{Selector: selector},
			)

			if err == nil {

				for i := range filteredList.Items {
					matching = append(matching, &filteredList.Items[i])
				}
			}
		}
	}

	return matching
}

// +kubebuilder:rbac:groups="",resources="configmaps",verbs={create,patch}

// reconcilePGAdminConfigMap writes the ConfigMap...
func (r *PGAdminReconciler) reconcilePGAdminConfigMap(
	ctx context.Context,
	pgAdmin *v1beta1.PGAdmin,
	clusters []*v1beta1.PostgresCluster,
) error {
	pgAdminConfigMap := &corev1.ConfigMap{ObjectMeta: naming.StandalonePGAdminClusters(pgAdmin)}
	pgAdminConfigMap.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))

	r.setControllerReference(pgAdmin, pgAdminConfigMap)

	pgAdminConfigMap.Annotations = naming.Merge(pgAdmin.Spec.Metadata.GetAnnotationsOrNil())
	pgAdminConfigMap.Labels = naming.Merge(pgAdmin.Spec.Metadata.GetLabelsOrNil(),
		map[string]string{
			naming.LabelRole: naming.RolePGAdmin,
		})

	err := clusterConfigMap(pgAdminConfigMap, clusters)

	if err == nil {
		err = errors.WithStack(r.apply(ctx, pgAdminConfigMap))
	}

	return err
}

// clusterConfigMap populates a clusterConfigMap with the configuration needed to run pgAdmin.
func clusterConfigMap(
	outConfigMap *corev1.ConfigMap,
	clusters []*v1beta1.PostgresCluster,
) error {
	initialize.StringMap(&outConfigMap.Data)

	// To avoid spurious reconciles, the following value must not change when
	// the spec does not change. [json.Encoder] and [json.Marshal] do this by
	// emitting map keys in sorted order. Indent so the value is not rendered
	// as one long line by `kubectl`.
	buffer := new(bytes.Buffer)
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	clusterServers := map[int]any{}
	for i, cluster := range clusters {
		object := map[string]any{
			"Name":          cluster.Name,
			"Group":         "Crunchy PostgreSQL Operator",
			"Host":          fmt.Sprintf("%s-primary.%s.svc", cluster.Name, cluster.Namespace),
			"Port":          5432,
			"MaintenanceDB": "postgres",
			"Username":      cluster.Name,
			"SSLMode":       "prefer",
			"Shared":        true,
		}
		clusterServers[i+1] = object
	}
	jsonWrap := map[string]any{
		"Servers": clusterServers,
	}
	err := encoder.Encode(jsonWrap)
	if err == nil {
		outConfigMap.Data["db.json"] = buffer.String()
	}
	return err
}

// +kubebuilder:rbac:groups="",resources="secrets",verbs={get}
// +kubebuilder:rbac:groups="",resources="secrets",verbs={create,delete,patch}

// reconcilePGAdminSecret reconciles the secret containing authentication
// for the pgAdmin administrator account
func (r *PGAdminReconciler) reconcilePGAdminSecret(
	ctx context.Context,
	pgadmin *v1beta1.PGAdmin) (*corev1.Secret, error) {

	existing := &corev1.Secret{ObjectMeta: naming.StandalonePGAdmin(pgadmin)}
	err := errors.WithStack(
		r.Client.Get(ctx, client.ObjectKeyFromObject(existing), existing))
	if client.IgnoreNotFound(err) != nil {
		return nil, err
	}

	intent := &corev1.Secret{ObjectMeta: naming.StandalonePGAdmin(pgadmin)}
	intent.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))

	intent.Annotations = naming.Merge(
		pgadmin.Spec.Metadata.GetAnnotationsOrNil(),
	)
	// TODO(benjb): think labels
	intent.Labels = naming.Merge(
		pgadmin.Spec.Metadata.GetLabelsOrNil(),
		map[string]string{
			naming.LabelCluster: pgadmin.Name,
			naming.LabelRole:    naming.RolePGAdmin,
		})

	intent.Data = make(map[string][]byte)

	// Copy existing password into the intent
	if existing.Data != nil {
		intent.Data["password"] = existing.Data["password"]
	}

	// When password is unset, generate a new one
	if len(intent.Data["password"]) == 0 {
		password, err := util.GenerateASCIIPassword(util.DefaultGeneratedPasswordLength)
		if err != nil {
			return nil, err
		}
		intent.Data["password"] = []byte(password)
	}

	r.setControllerReference(pgadmin, intent)
	err = errors.WithStack(r.apply(ctx, intent))
	if err == nil {
		return intent, nil
	}

	return nil, err
}
