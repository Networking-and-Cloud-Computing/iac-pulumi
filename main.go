package main

import (
	"encoding/json"
	"github.com/c-robinson/iplib"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/rds"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/route53"
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
		echo "logging.level.org.springframework.security=info"
		echo "env.domain=localhost"
	} >> /opt/application.properties
	{
		sudo /opt/aws/amazon-cloudwatch-agent/bin/amazon-cloudwatch-agent-ctl \
    		-a fetch-config \
    		-m ec2 \
    		-c file:/opt/cloudwatch-config.json \
    		-s
	}`
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
		domainName := c.Require("domain")
		dbName := c.Require("dbName")
		dbUserName := c.Require("dbUserName")
		dbPassword := c.Require("dbPassword")
		privateSubnetGroupName := c.Require("privateSubnetGroupName")
		rdsInstanceName := c.Require("rdsInstanceName")
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

		privateSubnetGroup, err := rds.NewSubnetGroup(ctx, privateSubnetGroupName, &rds.SubnetGroupArgs{
			SubnetIds: subnetIds,
			Tags: pulumi.StringMap{
				"Name": pulumi.String(privateSubnetGroupName),
			},
		})
		if err != nil {
			return err
		}
		// Create RDS Instance
		//
		rdsInstance, err := rds.NewInstance(ctx, rdsInstanceName, &rds.InstanceArgs{
			AllocatedStorage:    pulumi.Int(20),
			Engine:              pulumi.String("postgres"),
			EngineVersion:       pulumi.String("12"),
			ParameterGroupName:  parameterGroup.Name,
			VpcSecurityGroupIds: pulumi.StringArray{databaseSecurityGroup.ID()},
			InstanceClass:       pulumi.String("db.t2.micro"),
			DbName:              pulumi.String(dbName),
			Username:            pulumi.String(dbUserName),
			Password:            pulumi.String(dbPassword),
			SkipFinalSnapshot:   pulumi.Bool(true),
			MultiAz:             pulumi.Bool(false),
			PubliclyAccessible:  pulumi.Bool(false),
			DbSubnetGroupName:   privateSubnetGroup.Name,
			//Tags: pulumi.StringMap{
			//	"Name": pulumi.String(rdsInstanceName),
			//},
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

		// Get the zone created
		zoneID, err := route53.LookupZone(ctx, &route53.LookupZoneArgs{
			Name: pulumi.StringRef(domainName),
		}, nil)

		if err != nil {
			return err
		}

		// Create a new Role
		tmpJSON0, err := json.Marshal(map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{
				map[string]interface{}{
					"Action": "sts:AssumeRole",
					"Effect": "Allow",
					"Sid":    "",
					"Principal": map[string]interface{}{
						"Service": "ec2.amazonaws.com",
					},
				},
			},
		})
		if err != nil {
			return err
		}
		json0 := string(tmpJSON0)
		role, err := iam.NewRole(ctx, "death", &iam.RoleArgs{
			AssumeRolePolicy: pulumi.String(json0),
			Tags: pulumi.StringMap{
				"tag-key": pulumi.String("tag-value"),
			},
		})
		if err != nil {
			return err
		}
		// Create a new IAM instance profile with the created IAM role.
		instanceProfile, err := iam.NewInstanceProfile(ctx, "instanceProfile", &iam.InstanceProfileArgs{
			Role: role.Name,
		})
		if err != nil {
			return err
		}
		// Attach the new Role
		_, err = iam.NewRolePolicyAttachment(ctx, "myRolePolicyAttachment", &iam.RolePolicyAttachmentArgs{
			Role:      role.Name,
			PolicyArn: pulumi.String("arn:aws:iam::aws:policy/CloudWatchAgentServerPolicy"),
		})
		if err != nil {
			return err
		}

		instance, err := ec2.NewInstance(ctx, "HelloWorld-1", &ec2.InstanceArgs{
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
			IamInstanceProfile: instanceProfile.ID(),
			RootBlockDevice: &ec2.InstanceRootBlockDeviceArgs{
				VolumeSize: pulumi.Int(25),
				VolumeType: pulumi.String("gp2"),
			},
			Tags: pulumi.StringMap{
				"Name": pulumi.String("PHOENIX"),
			},
		})
		if err != nil {
			return err
		}

		////Create a Load Balancer
		//lb, err := elb.NewLoadBalancer(ctx, "LoadBalancer", &elb.LoadBalancerArgs{
		//	//AvailabilityZones: pulumi.StringArray{
		//	//	pulumi.String("us-east-1a"),
		//	//},
		//	Listeners: elb.LoadBalancerListenerArray{
		//		&elb.LoadBalancerListenerArgs{
		//			InstancePort:     pulumi.Int(80),
		//			InstanceProtocol: pulumi.String("http"),
		//			LbPort:           pulumi.Int(80),
		//			LbProtocol:       pulumi.String("http"),
		//		},
		//	},
		//	Subnets:   pulumi.StringArray{publicsubnetIds[0]},
		//	Instances: pulumi.StringArray{instance.ID()},
		//})
		//if err != nil {
		//	return err
		//}

		// Create a new A Record
		_, err = route53.NewRecord(ctx, "A-RECORD", &route53.RecordArgs{
			Name:    pulumi.String(domainName),
			Type:    pulumi.String("A"),
			Ttl:     pulumi.Int(60),
			ZoneId:  pulumi.String(zoneID.Id),
			Records: pulumi.StringArray{instance.PublicIp},
			//Aliases: route53.RecordAliasArray{
			//	&route53.RecordAliasArgs{
			//		EvaluateTargetHealth: pulumi.Bool(true),
			//		Name:                 instance.PublicDns,
			//		ZoneId:               instance.ZoneId,
			//		//.ToStringOutput().ApplyT(func(zoneId string) pulumi.StringInput {
			//		//		return pulumi.String(zoneId)
			//		//	}).(pulumi.StringInput),
			//	},
			//},
		})
		if err != nil {
			return err
		}

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
