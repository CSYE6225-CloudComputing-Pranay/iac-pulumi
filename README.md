# iac-pulumi
Repository to handle infrastructure using Pulumi

# Pulumi AWS EC2 Instance Deployment in Specified VPC

This repository contains the Pulumi code and configuration to deploy an Amazon Web Services (AWS) EC2 instance within a specified Virtual Private Cloud (VPC).

## Prerequisites

Before you begin, make sure you have the following prerequisites installed on your local machine:

- [Pulumi](https://www.pulumi.com/docs/get-started/install/)
- [AWS CLI](https://aws.amazon.com/cli/) with valid AWS credentials configured

## Getting Started

1. Clone this repository to your local machine.


2. Initialize a new Pulumi project within the repository.

```bash
pulumi new aws-go
```

3. Configure your Pulumi stack to use the desired AWS region and VPC details. Update the `Pulumi.dev.yaml` or any other stack configuration file as needed.

```yaml
config:
  aws:profile: <your-aws-profile>
  aws:region: <your-aws-region>
```


4. Initialize and deploy your Pulumi stack.

```bash
pulumi up
```

Review the changes and confirm the deployment when prompted.

5Pulumi will provision the specified EC2 instance within the VPC. Once the deployment is complete, you will see the EC2 instance's public IP address and other relevant information in the output.

## Destroying the Stack

If you want to tear down the deployed infrastructure, you can do so with the following command:

```bash
pulumi destroy
```

Review the changes and confirm the destruction when prompted.


## Important Notes

- Ensure you have the necessary IAM permissions to create and manage AWS resources within the specified VPC.
- Review and update security groups, IAM roles, and other security configurations as needed to meet your specific requirements.
- Be mindful of the costs associated with running AWS resources, especially EC2 instances. Be sure to destroy resources when they are no longer needed to avoid unnecessary charges.