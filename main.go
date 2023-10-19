package main

import (
	"github.com/c-robinson/iplib"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
	"net"
	"strconv"
	"strings"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		c := config.New(ctx, "")
		cidrBlock := c.Require("cidrBlock")
		vpcName := c.Require("vpcName")
		destinationBlock := c.Require("destinationBlock")
		publicSubnet := c.Require("publicSubnetName")
		privateSubnet := c.Require("privateSubnetName")
		internetGatewayName := c.Require("internetGatewayName")
		publicRouteTableName := c.Require("publicRouteTableName")
		privateRouteTableName := c.Require("privateRouteTableName")
		publicRouteAssociationName := c.Require("publicRouteAssociationName")
		privateRouteAssociationName := c.Require("privateRouteAssociationName")
		instanceType := c.Require("instanceType")
		publicSubnetID := c.Require("publicSubnetID")
		amiID := c.Require("amiID")
		availabilityZones, err := aws.GetAvailabilityZones(ctx, &aws.GetAvailabilityZonesArgs{
			State: pulumi.StringRef("available"),
		}, nil)
		if err != nil {
			return err
		}

		zoneCount := len(availabilityZones.Names)
		subnetCount := min(zoneCount, 3)
		// Create a VPC
		vpc, err := ec2.NewVpc(ctx, vpcName, &ec2.VpcArgs{
			CidrBlock: pulumi.String(cidrBlock),
			Tags: pulumi.StringMap{
				"Name": pulumi.String(vpcName),
			},
		})
		if err != nil {
			return err
		}

		parts := strings.Split(cidrBlock, "/")
		ip := parts[0]
		maskStr := parts[1]
		mask, _ := strconv.Atoi(maskStr)

		n := iplib.NewNet4(net.ParseIP(ip), mask)
		subnets, _ := n.Subnet(24)

		subnetStrings := make([]string, len(subnets))
		for i, subnet := range subnets {
			subnetStrings[i] = subnet.String()
		}
		//Create an ec2 Security Group
		securityGroup, err := ec2.NewSecurityGroup(ctx, "webSecurityGroup", &ec2.SecurityGroupArgs{
			Description: pulumi.String("Enable HTTP and SSH access"),
			Egress:      ec2.SecurityGroupEgressArray{egressArgs("0.0.0.0/0", "all")},
			Ingress: ec2.SecurityGroupIngressArray{
				ingressArgs("0.0.0.0/0", "tcp", 22),
				ingressArgs("0.0.0.0/0", "tcp", 80),
				ingressArgs("0.0.0.0/0", "tcp", 443),
				// Add additional port number that your application runs on.
				ingressArgs("0.0.0.0/0", "tcp", 8080),
			},
			Tags: pulumi.StringMap{
				"Name": pulumi.String("application_security_group"),
			},
		})

		if err != nil {
			return err
		}
		//// Look Up for the AMI
		//ami, err := ec2.LookupAmi(ctx, &ec2.LookupAmiArgs{
		//	MostRecent: pulumi.BoolRef(true),
		//	Filters: []ec2.GetAmiFilter{
		//		{
		//			Name: "name",
		//			Values: []string{
		//				"Cloud",
		//			},
		//		},
		//		{
		//			Name: "virtualization-type",
		//			Values: []string{
		//				"hvm",
		//			},
		//		},
		//	},
		//	Owners: []string{
		//		"172266639576",
		//	},
		//}, nil)
		//if err != nil {
		//	return err
		//}
		// Create a New Ec2 Instance with the above Security Group
		_, err = ec2.NewInstance(ctx, "Cloud", &ec2.InstanceArgs{
			InstanceType:          pulumi.String(instanceType),
			SubnetId:              pulumi.String(publicSubnetID),
			VpcSecurityGroupIds:   pulumi.StringArray{securityGroup.ID()},
			Ami:                   pulumi.String(amiID),
			KeyName:               pulumi.String("Cloud"),
			DisableApiTermination: pulumi.Bool(false),
			RootBlockDevice: &ec2.InstanceRootBlockDeviceArgs{
				VolumeSize: pulumi.Int(25),
				VolumeType: pulumi.String("gp2"),
			},
			Tags: pulumi.StringMap{
				"Name": pulumi.String("Cloud-EC2-Instance"),
			},
		})
		if err != nil {
			return err
		}
		//ctx.Export("instanceID", instance.ID())
		// Create 3 Public Subnets

		publicSubnets := make([]*ec2.Subnet, 0, subnetCount)
		for i := 0; i < subnetCount; i++ {
			publicSubnet, err := ec2.NewSubnet(ctx, publicSubnet+strconv.Itoa(i), &ec2.SubnetArgs{
				VpcId:               vpc.ID(),
				CidrBlock:           pulumi.String(subnetStrings[i]),
				AvailabilityZone:    pulumi.String(availabilityZones.Names[i]),
				MapPublicIpOnLaunch: pulumi.Bool(true),
				Tags: pulumi.StringMap{
					"Name": pulumi.String(publicSubnet + strconv.Itoa(i)),
				},
			})
			if err != nil {
				return err
			}
			publicSubnets = append(publicSubnets, publicSubnet)
		}

		// Create 3 Private Subnets
		privateSubnets := make([]*ec2.Subnet, 0, subnetCount)
		for i := 0; i < subnetCount; i++ {
			privateSubnet, err := ec2.NewSubnet(ctx, privateSubnet+strconv.Itoa(i), &ec2.SubnetArgs{
				VpcId:            vpc.ID(),
				CidrBlock:        pulumi.String(subnetStrings[i+subnetCount]),
				AvailabilityZone: pulumi.String(availabilityZones.Names[i]),
				Tags: pulumi.StringMap{
					"Name": pulumi.String(privateSubnet + strconv.Itoa(i)),
				},
			})
			if err != nil {
				return err
			}
			privateSubnets = append(privateSubnets, privateSubnet)
		}

		// Create a Internet gateway
		internetGateway, err := ec2.NewInternetGateway(ctx, internetGatewayName, &ec2.InternetGatewayArgs{
			VpcId: vpc.ID(),
			Tags: pulumi.StringMap{
				"Name": pulumi.String(internetGatewayName),
			},
		})
		if err != nil {
			return err
		}

		//Create a Public Route Table
		publicRouteTable, err := ec2.NewRouteTable(ctx, publicRouteTableName, &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Tags: pulumi.StringMap{
				"Name": pulumi.String(publicRouteTableName),
			},
		})
		if err != nil {
			return err
		}
		// Create a Private Route Table
		privateRouteTable, err := ec2.NewRouteTable(ctx, privateRouteTableName, &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Tags: pulumi.StringMap{
				"Name": pulumi.String(privateRouteTableName),
			},
		})
		if err != nil {
			return err
		}
		// Associate the Public Subnets to the Public Route Table.
		for i, subnet := range publicSubnets {
			_, err := ec2.NewRouteTableAssociation(ctx, publicRouteAssociationName+strconv.Itoa(i), &ec2.RouteTableAssociationArgs{
				SubnetId:     subnet.ID(),
				RouteTableId: publicRouteTable.ID(),
			})
			if err != nil {
				return err
			}
		}

		// Associate the Private Subnets to the Private Route Table.
		for i, subnet := range privateSubnets {
			_, err := ec2.NewRouteTableAssociation(ctx, privateRouteAssociationName+strconv.Itoa(i), &ec2.RouteTableAssociationArgs{
				SubnetId:     subnet.ID(),
				RouteTableId: privateRouteTable.ID(),
			})
			if err != nil {
				return err
			}
		}
		public_route, err := ec2.NewRoute(ctx, "public-route", &ec2.RouteArgs{
			RouteTableId:         publicRouteTable.ID(),
			DestinationCidrBlock: pulumi.String(destinationBlock),
			GatewayId:            internetGateway.ID(),
		})
		if err != nil {
			return err
		}
		ctx.Export("PublicRouteID", public_route.ID())
		return err

	})
}

func ingressArgs(cidr, protocol string, fromPort int) ec2.SecurityGroupIngressInput {
	return ec2.SecurityGroupIngressArgs{
		Protocol:   pulumi.String(protocol),
		FromPort:   pulumi.Int(fromPort),
		ToPort:     pulumi.Int(fromPort),
		CidrBlocks: pulumi.StringArray{pulumi.String(cidr)},
	}
}
func egressArgs(cidr, protocol string) ec2.SecurityGroupEgressInput {
	return ec2.SecurityGroupEgressArgs{
		Protocol:   pulumi.String(protocol),
		FromPort:   pulumi.Int(0),
		ToPort:     pulumi.Int(0),
		CidrBlocks: pulumi.StringArray{pulumi.String(cidr)},
	}
}
