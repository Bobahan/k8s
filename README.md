# Deploying Go and PostgreSQL Applications Locally with Minikube
Before installing minikube, make sure you already have a container runtime installed, such as Docker.

You can find the official installation and startup guide for minikube here: [minukube](https://minikube.sigs.k8s.io/docs/start/?arch=%2Fmacos%2Farm64%2Fstable%2Fbinary+download)

minikube is a local Kubernetes environment primarily used for running and testing applications locally. It usually runs as a single-node cluster, where one machine acts as both the control plane and the worker node.

Start `minikube` and check its status:
```bash
minikube start
minikube status
```

If minikube is running, let’s dive deeper.

# Metrics-server Addon
We need to enable the `metrics-server` addon:
```bash
minikube addons enable metrics-server
```

`metrics-server` allows Kubernetes to collect resource usage metrics such as CPU and memory. These metrics are required for the **HorizontalPodAutoscaler (HPA)**, which can automatically scale the number of Pods based on CPU utilization.

In our case, the HPA can scale the number of Pods up to **10** if CPU usage exceeds the configured `averageUtilization` threshold.

Select one of the Go application Pods:
```bash
kubectl get pods
```

Run a simple CPU stress test inside one of the Pods using `md5sum`:
```bash
kubectl exec -it <pod-name> -- md5sum /dev/zero
```

At the same time, if you keep refreshing the application in your browser, you should see requests being distributed across multiple Go Pods.
```bash
kubectl get hpa -w
```

The policy behind Pods is to increase Pod quickly but decrease slowly

# Ingress Controller Addon
Enable the `Ingress Controller` addon so that the application can be accessed from a browser:
```bash
minikube addons enable ingress
```

Add the following entry to your `/etc/hosts` file:
```bash
sudo nano /etc/hosts
127.0.0.1 golang.test # this one
```

Then open a tunnel and keep it running:
```bash
sudo minikube tunnel
```

# Policies
We use the `RollingUpdate` strategy.  
This means that when a new version of the application is deployed, Kubernetes gradually replaces old Pods with new ones instead of recreating everything at once.

During the update, the `ReplicaSet` can temporarily create an additional Pod before terminating one of the old ones. Because of this, the total number of running Pods can briefly increase — for example, from **3** to **4**.

Once the new Pod passes its `readinessProbe`, the `Service` starts routing traffic to it.  
After that, Kubernetes can safely terminate one of the old Pods.

Another important configuration is `topologySpreadConstraints`.  
It helps distribute Pods evenly across available Nodes so that the difference in the number of Pods between Nodes does not exceed **1**.

If Kubernetes cannot fully satisfy this distribution rule, the scheduler will still place the Pod according to the defined scheduling policy.

# Build the Docker Image
Our Go application needs to be packaged into a Docker image and built locally.  
We’ll also assign a tag so the image can be loaded into Minikube.
```bash
docker build -f deploy/docker/Dockerfile -t go-processing:1.0 .
minikube image load go-processing:1.0
```

After that, update the image version in the `golang.yaml` file:
```yml
image: go-processing:v1.0
```

# Deploy PostgreSQL with CloudNativePG
Now that the local Kubernetes environment is ready, we can apply the required manifests and install `CloudNativePG` to deploy a PostgreSQL cluster.

First, apply the database-related configuration files:
```bash
kubectl apply -f pg-superuser-secret.yaml
kubectl apply -f pg-user-secret.yaml
kubectl apply -f pg-configmap.yaml
```

Then install the `CloudNativePG` operator:
```bash
kubectl apply --server-side -f https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/main/releases/cnpg-1.26.1.yaml
```

After that, deploy the PostgreSQL cluster and the application resources:
```bash
kubectl apply -f pg-cluster.yaml
kubectl apply -f golang.yaml
kubectl apply -f hpa.yaml
kubectl apply -f ingress.yaml
```

# Create a Table
### Connect to the Master
Forward the PostgreSQL service port from the cluster to your local machine for the **Master**:
```bash
kubectl port-forward svc/pg-cluster-rw 5432:5432
```

On macOS, make sure `libpq` is linked locally:
```bash
brew link --force libpq
psql --version
```

Connect to PostgreSQL:
```
psql -h localhost -U app -d app
```

Enter the password when prompted.

Create a test table:
```sql
CREATE TABLE test (
  id serial primary key,
  name text
);

INSERT INTO test(name) VALUES ('hello from primary');

SELECT * FROM test;
```

---
### Connect to the Replica
Forward the port for the **Replica**:
```
kubectl port-forward svc/pg-cluster-ro 5433:5432
```

Connect to the replica:
```
psql -h localhost -p 5433 -U app -d app
```

✅ Verify that the records from the **Master** are present:
```sql
SELECT * FROM test;
SELECT pg_is_in_recovery();
```

If `pg_is_in_recovery()` returns `t`, it means you are connected to a **replica**.

Attempting to insert a record on the replica will fail:
```sql
INSERT INTO test(name) VALUES ('should fail');
```

❌ The operation will produce an error:
```
ERROR:  cannot execute INSERT in a read-only transaction
```