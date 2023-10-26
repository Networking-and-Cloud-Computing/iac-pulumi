package main

import (
	"github.com/c-robinson/iplib"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/rds"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
	"net"
	"strconv"
	"strings"
)

func main() {
	userData := `#!/bin/bash
	{
		echo "spring.jpa.hibernate.ddl-auto=update"
		echo "spring.datasource.url=jdbc:postgresql://${HOST}/${DB_Name}"
		echo "spring.datasource.username=${DB_USER}"
		echo "spring.datasource.password=${DB_PASSWORD}"
		echo "spring.profiles.active=development"
		echo "spring.jpa.show-sql=true"
		echo "logging.level.org.springframework.security=info"
	} >> /opt/application.properties`
	userData = strings.Replace(userData, "${DB_Name}", "Joshi", -1)
	userData = strings.Replace(userData, "${DB_USER}", "cjoshi", -1)
	userData = strings.Replace(userData, "${DB_PASSWORD}", "Password123", -1)

	pulumi.Run(func(ctx *pulumi.Context) error {
		c := config.New(ctx, "")
		cidrBlock := c.Require("cidrBlock")
		vpcName := c.Require("vpcName")
		destinationBlock := c.Require("destinationBlock")
		publicSubnet := c.Require("publicSubnetName")
		privateSubnetName := c.Require("privateSubnetName")
		internetGatewayName := c.Require("internetGatewayName")
		publicRouteTableName := c.Require("publicRouteTableName")
		privateRouteTableName := c.Require("privateRouteTableName")
		publicRouteAssociationName := c.Require("publicRouteAssociationName")
		privateRouteAssociationName := c.Require("privateRouteAssociationName")
		instanceType := c.Require("instanceType")
		//publicSubnetID := c.Require("publicSubnetID")
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

		// Create 3 Private Subnets
		privateSubnets := make([]*ec2.Subnet, 0, subnetCount)
		for i := 0; i < subnetCount; i++ {
			privateSubnet, err := ec2.NewSubnet(ctx, privateSubnetName+strconv.Itoa(i), &ec2.SubnetArgs{
				VpcId:            vpc.ID(),
				CidrBlock:        pulumi.String(subnetStrings[i+subnetCount]),
				AvailabilityZone: pulumi.String(availabilityZones.Names[i]),
				Tags: pulumi.StringMap{
					"Name": pulumi.String(privateSubnetName + strconv.Itoa(i)),
				},
			})
			if err != nil {
				return err
			}
			privateSubnets = append(privateSubnets, privateSubnet)
		}

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

		var publicsubnetIds pulumi.StringArray
		for i := range publicSubnets {
			publicsubnetIds = append(publicsubnetIds, publicSubnets[i].ID())
		}
		//Create an ec2 Security Group
		securityGroup, err := ec2.NewSecurityGroup(ctx, "webSecurityGroup", &ec2.SecurityGroupArgs{
			Description: pulumi.String("Enable HTTP and SSH access"),
			VpcId:       vpc.ID(),
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

		//Create DB Security Group
		databaseSecurityGroup, err := ec2.NewSecurityGroup(ctx, "dbSecurityGroup", &ec2.SecurityGroupArgs{
			Description: pulumi.String("Enable Database Access"),
			VpcId:       vpc.ID(),
			Ingress: ec2.SecurityGroupIngressArray{&ec2.SecurityGroupIngressArgs{

				Protocol:       pulumi.String("tcp"),
				FromPort:       pulumi.Int(5432),
				ToPort:         pulumi.Int(5432),
				SecurityGroups: pulumi.StringArray{securityGroup.ID()},
			}},
		})
		if err != nil {
			return err
		}
		_, err = ec2.NewSecurityGroupRule(ctx, "application-security-group-egress-rule", &ec2.SecurityGroupRuleArgs{
			FromPort:              pulumi.Int(5432),
			ToPort:                pulumi.Int(5432),
			Protocol:              pulumi.String("tcp"),
			Type:                  pulumi.String("egress"),
			SourceSecurityGroupId: databaseSecurityGroup.ID(),
			SecurityGroupId:       securityGroup.ID(),
		})
		if err != nil {
			return err
		}
		//Create a parameter Group
		parameterGroup, err := rds.NewParameterGroup(ctx, "why-gawdd-why", &rds.ParameterGroupArgs{
			Description: pulumi.String("Custom Parameter Group"),
			Family:      pulumi.String("postgres12"),
			Name:        pulumi.String("why-gawdd-why"),
		})
		if err != nil {
			return err
		}

		var subnetIds pulumi.StringArray
		for i := range privateSubnets {
			subnetIds = append(subnetIds, privateSubnets[i].ID())
		}

		privateSubnetGroup, err := rds.NewSubnetGroup(ctx, "whyareyoustillalive", &rds.SubnetGroupArgs{
			SubnetIds: subnetIds,
			Tags: pulumi.StringMap{
				"Name": pulumi.String("whyareyoustillalive"),
			},
		})
		if err != nil {
			return err
		}
		// Create RDS Instance
		//
		rdsInstance, err := rds.NewInstance(ctx, "please-work", &rds.InstanceArgs{
			AllocatedStorage:    pulumi.Int(20),
			Engine:              pulumi.String("postgres"),
			EngineVersion:       pulumi.String("12"),
			ParameterGroupName:  parameterGroup.Name,
			VpcSecurityGroupIds: pulumi.StringArray{databaseSecurityGroup.ID()},
			InstanceClass:       pulumi.String("db.t2.micro"),
			DbName:              pulumi.String("Joshi"),
			Username:            pulumi.String("cjoshi"),
			Password:            pulumi.String("Password123"),
			SkipFinalSnapshot:   pulumi.Bool(true),
			MultiAz:             pulumi.Bool(false),
			PubliclyAccessible:  pulumi.Bool(false),
			DbSubnetGroupName:   privateSubnetGroup.Name,
		})
		if err != nil {
			return err
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
		_, err = ec2.NewInstance(ctx, "IAMDEAD", &ec2.InstanceArgs{
			InstanceType:          pulumi.String(instanceType),
			SubnetId:              publicsubnetIds[0],
			VpcSecurityGroupIds:   pulumi.StringArray{securityGroup.ID()},
			Ami:                   pulumi.String(amiID),
			KeyName:               pulumi.String("Cloud"),
			DisableApiTermination: pulumi.Bool(false),
			UserData: rdsInstance.Endpoint.ApplyT(
				func(args interface{}) (string, error) {
					endpoint := args.(string)
					userData = strings.Replace(userData, "${HOST}", endpoint, -1)
					return userData, nil
				},
			).(pulumi.StringOutput),
			RootBlockDevice: &ec2.InstanceRootBlockDeviceArgs{
				VolumeSize: pulumi.Int(25),
				VolumeType: pulumi.String("gp2"),
			},
			Tags: pulumi.StringMap{
				"Name": pulumi.String("IAMDEAD"),
			},
		})
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
