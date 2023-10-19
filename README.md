# iac-pulumi

This Pulumi program creates a VPC and two subnets, one public and one private, within AWS using the Go SDK.

Resources Created
The following resources are created by the Pulumi program:

VPC: A new VPC is created with a CIDR block of 10.0.0.0/16, DNS support enabled, and DNS hostnames enabled. It is tagged with the name "my-vpc".

Public Subnet: A new public subnet is created within the VPC with a CIDR block of 10.0.1.0/24. It is tagged with the name "my-public-subnet". This subnet is configured to auto-assign public IPs to instances that are launched within it.

Private Subnet: A new private subnet is created within the VPC with a CIDR block of 10.0.2.0/24. It is tagged with the name "my-private-subnet".

The IDs of these created resources are exported by the Pulumi program, and they can be used as inputs to other Pulumi programs or AWS resources.

Prerequisites
Before running the Pulumi program, validate the following:

You have installed the pulumi command line interface You have configured AWS credentials in your local environment Running the Pulumi Program Run pulumi up command in your terminal from the directory where you have this Pulumi program. This will start the provisioning of the AWS resources in your default AWS region.

Press y or enter when you get the prompt Do you want to perform this update? to confirm and proceed with creating the resources.
