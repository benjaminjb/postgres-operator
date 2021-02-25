/*
 Copyright 2021 Crunchy Data Solutions, Inc.
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

package v1alpha1

import (
	"reflect"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/yaml"
)

func TestPostgresClusterWebhooks(t *testing.T) {
	var _ webhook.Defaulter = new(PostgresCluster)
}

func TestPostgresClusterDefault(t *testing.T) {
	t.Run("TypeMeta", func(t *testing.T) {
		var cluster PostgresCluster
		cluster.Default()

		assert.Equal(t, cluster.APIVersion, GroupVersion.String())
		assert.Equal(t, cluster.Kind, reflect.TypeOf(cluster).Name())
	})

	t.Run("no instance sets", func(t *testing.T) {
		var cluster PostgresCluster
		cluster.Default()

		b, err := yaml.Marshal(cluster)
		assert.NilError(t, err)
		assert.DeepEqual(t, string(b), strings.TrimSpace(`
apiVersion: postgres-operator.crunchydata.com/v1alpha1
kind: PostgresCluster
metadata:
  creationTimestamp: null
spec:
  archive:
    pgbackrest:
      repoHost:
        resources: {}
      repoVolume: {}
  instances: null
  patroni:
    dynamicConfiguration: null
    leaderLeaseDurationSeconds: 30
    port: 8008
    syncPeriodSeconds: 10
  port: 5432
status: {}
		`)+"\n")
	})

	t.Run("one instance set", func(t *testing.T) {
		var cluster PostgresCluster
		cluster.Spec.InstanceSets = []PostgresInstanceSetSpec{{}}
		cluster.Default()

		b, err := yaml.Marshal(cluster)
		assert.NilError(t, err)
		assert.DeepEqual(t, string(b), strings.TrimSpace(`
apiVersion: postgres-operator.crunchydata.com/v1alpha1
kind: PostgresCluster
metadata:
  creationTimestamp: null
spec:
  archive:
    pgbackrest:
      repoHost:
        resources: {}
      repoVolume: {}
  instances:
  - name: "00"
    replicas: 1
    resources: {}
  patroni:
    dynamicConfiguration: null
    leaderLeaseDurationSeconds: 30
    port: 8008
    syncPeriodSeconds: 10
  port: 5432
status: {}
		`)+"\n")
	})
}

func TestPostgresInstanceSetSpecDefault(t *testing.T) {
	var spec PostgresInstanceSetSpec
	spec.Default(5)

	b, err := yaml.Marshal(spec)
	assert.NilError(t, err)
	assert.DeepEqual(t, string(b), strings.TrimSpace(`
name: "05"
replicas: 1
resources: {}
	`)+"\n")
}