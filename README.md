Uniflow Operator

The Uniflow Operator is a Kubernetes controller designed for managing Service and Revision resources. It tracks workflow changes and ensures seamless deployment of serverless workloads using Knative. This operator simplifies the orchestration and versioning of workflows in dynamic environments.

Features

Dynamic Revision Management

	•	Automatically creates and manages Revision resources based on Service updates.
	•	Supports version control for services and workflows.

Efficient Resource Cleanup

	•	Automatically removes outdated Revisions while retaining the latest version.
	•	Ensures optimal resource utilization.

Knative Integration

	•	Leverages Knative Serving to provide serverless deployment for workflows.
	•	Supports real-time and scalable workload management.

Event-Driven Reconciliation

	•	Listens to workflow changes and dynamically updates Kubernetes resources.
	•	Ensures system state aligns with user-defined specifications.

Getting Started

Prerequisites

	•	Kubernetes 1.24 or later
	•	Knative Serving installed
	•	Helm 3 (optional for installation)

Installation

1. Clone the Repository

git clone https://github.com/siyul-park/uniflow-operator.git
cd uniflow-operator

2. Deploy Custom Resource Definitions (CRDs)

kubectl apply -f config/crd/bases

3. Deploy the Controller

kubectl apply -f config/manager

4. Verify Deployment

kubectl get pods -n uniflow-system

Usage

Define a Service

Create a Service manifest:

apiVersion: uniflow.dev/v1
kind: Service
metadata:
  name: example-service
spec:
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
        - name: example-container
          image: example-image:latest

Apply the configuration:

kubectl apply -f service.yaml

Monitor Revisions

Check created Revisions:

kubectl get revisions -n <namespace>

Inspect Service status:

kubectl describe service example-service

Development

Local Development

	1.	Install Dependencies

go mod tidy


	2.	Run the Controller

make run


	3.	Apply Resources Locally

kubectl apply -f examples/service.yaml



Run Tests

make test

Contributing

Steps to Contribute

	1.	Fork the repository.
	2.	Create a feature branch:

git checkout -b feature/my-feature


	3.	Commit your changes:

git commit -m "Add my feature"


	4.	Push the branch:

git push origin feature/my-feature


	5.	Create a pull request in the repository.

Code Standards

	•	Follow the Go Code Style.
	•	Write tests for new features.
	•	Ensure code passes golangci-lint checks.

License

This project is licensed under the Apache License 2.0.

Contact

For questions or issues, please open an issue in the GitHub repository.
