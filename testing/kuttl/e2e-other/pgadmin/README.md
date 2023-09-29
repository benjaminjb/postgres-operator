** pgAdmin **

requires the FeatureGate

* create pgaadmin, check pgadmin, configmap
* dump servers -- 0

* create a cluster
* check cluster, configmap
* dump servers -- 1

* create a cluster
* check cluster, configmap
* dump servers -- 2

* delete a cluster
* check configmap
* dump servers -- 1
