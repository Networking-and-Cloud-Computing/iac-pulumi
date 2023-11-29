package main

import (
	"encoding/base64"
	"encoding/json"
	"github.com/c-robinson/iplib"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/alb"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/autoscaling"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/dynamodb"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lb"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/rds"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/route53"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/sns"
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/storage"
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
		echo "spring.datasource.hikari.connection-timeout=2000"
		echo "logging.level.org.springframework.security=info"
		echo "env.domain=localhost"
		echo "AWS_ACCESS_KEY_ID=AKIA46JJ7APOLFBEE2BC"
		echo "AWS_SECRET_ACCESS_KEY=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"
		echo "SUBMISSION_TOPIC_ARN=${SUBMISSION_TOPIC_ARN}"

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
		appSecurityGroupName := c.Require("appSecurityGroupName")
		domainName := c.Require("domain")
		dbSecurityGroupName := c.Require("dbSecurityGroupName")
		dbName := c.Require("dbName")
		dbUserName := c.Require("dbUserName")
		dbPassword := c.Require("dbPassword")
		privateSubnetGroupName := c.Require("privateSubnetGroupName")
		rdsInstanceName := c.Require("rdsInstanceName")
		lbSecurityGroup := c.Require("lbSecurityGroup")
		environment := c.Require("environment")
		paramsName := c.Require("paramsName")
		path := c.Require("path")
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

		//Create an instance Security Group
		webSecurityGroup, err := ec2.NewSecurityGroup(ctx, appSecurityGroupName, &ec2.SecurityGroupArgs{
			Description: pulumi.String("Enable HTTP and SSH access"),
			VpcId:       vpc.ID(),
			Egress:      ec2.SecurityGroupEgressArray{egressArgs("0.0.0.0/0", "all")},
			Ingress: ec2.SecurityGroupIngressArray{
				ingressArgs("0.0.0.0/0", "tcp", 22),
				// Add additional port number that your application runs on.
				ingressArgs("0.0.0.0/0", "tcp", 8080),
			},
			Tags: pulumi.StringMap{
				"Name": pulumi.String(appSecurityGroupName),
			},
		})
		if err != nil {
			return err
		}

		//Create DB Security Group
		databaseSecurityGroup, err := ec2.NewSecurityGroup(ctx, dbSecurityGroupName, &ec2.SecurityGroupArgs{
			Description: pulumi.String("Enable Database Access"),
			VpcId:       vpc.ID(),
			Ingress: ec2.SecurityGroupIngressArray{&ec2.SecurityGroupIngressArgs{
				Protocol:       pulumi.String("tcp"),
				FromPort:       pulumi.Int(5432),
				ToPort:         pulumi.Int(5432),
				SecurityGroups: pulumi.StringArray{webSecurityGroup.ID()},
			}},
			Tags: pulumi.StringMap{
				"Name": pulumi.String(dbSecurityGroupName),
			},
		})
		if err != nil {
			return err
		}

		//Security Group Egress Rule
		_, err = ec2.NewSecurityGroupRule(ctx, "application-security-group-egress-rule", &ec2.SecurityGroupRuleArgs{
			FromPort:              pulumi.Int(5432),
			ToPort:                pulumi.Int(5432),
			Protocol:              pulumi.String("tcp"),
			Type:                  pulumi.String("egress"),
			SourceSecurityGroupId: databaseSecurityGroup.ID(),
			SecurityGroupId:       webSecurityGroup.ID(),
		})
		if err != nil {
			return err
		}
		//Create a parameter Group
		parameterGroup, err := rds.NewParameterGroup(ctx, paramsName, &rds.ParameterGroupArgs{
			Description: pulumi.String("Custom Parameter Group"),
			Family:      pulumi.String("postgres12"),
			Name:        pulumi.String(paramsName),
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

		// Create an Internet gateway
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
		_, err = iam.NewRolePolicyAttachment(ctx, "myRolePolicyAttachment-SNS", &iam.RolePolicyAttachmentArgs{
			Role:      role.Name,
			PolicyArn: pulumi.String("arn:aws:iam::aws:policy/AmazonSNSFullAccess"),
		})
		if err != nil {
			return err
		}

		_, err = iam.NewRolePolicyAttachment(ctx, "myRolePolicyAttachment", &iam.RolePolicyAttachmentArgs{
			Role:      role.Name,
			PolicyArn: pulumi.String("arn:aws:iam::aws:policy/CloudWatchAgentServerPolicy"),
		})
		if err != nil {
			return err
		}

		//instance, err := ec2.NewInstance(ctx, "HelloWorld", &ec2.InstanceArgs{
		//	InstanceType:          pulumi.String(instanceType),
		//	SubnetId:              publicsubnetIds[0],
		//	VpcSecurityGroupIds:   pulumi.StringArray{webSecurityGroup.ID()},
		//	Ami:                   pulumi.String(amiID),
		//	KeyName:               pulumi.String("Cloud"),
		//	DisableApiTermination: pulumi.Bool(false),
		//	UserData: rdsInstance.Endpoint.ApplyT(
		//		func(args interface{}) (string, error) {
		//			endpoint := args.(string)
		//			userData = strings.Replace(userData, "${HOST}", endpoint, -1)
		//			return userData, nil
		//		},
		//	).(pulumi.StringOutput),
		//	IamInstanceProfile: instanceProfile.ID(),
		//	RootBlockDevice: &ec2.InstanceRootBlockDeviceArgs{
		//		VolumeSize: pulumi.Int(25),
		//		VolumeType: pulumi.String("gp2"),
		//	},
		//	Tags: pulumi.StringMap{
		//		"Name": pulumi.String("RDSCHECK-TO-DIE"),
		//	},
		//})
		//if err != nil {
		//	return err
		//}

		//Create a security group Rule for load balancer
		lbSecGroup, err := ec2.NewSecurityGroup(ctx, lbSecurityGroup, &ec2.SecurityGroupArgs{
			Description: pulumi.String("Load balancer security group"),
			Ingress: ec2.SecurityGroupIngressArray{
				ingressArgs("0.0.0.0/0", "tcp", 80),
				// Add additional port number that your application runs on.
				ingressArgs("0.0.0.0/0", "tcp", 443),
			},
			VpcId: vpc.ID(),
			// Allow all outbound traffic
			Egress: ec2.SecurityGroupEgressArray{
				ec2.SecurityGroupEgressArgs{
					Protocol:   pulumi.String("-1"), // All protocols
					FromPort:   pulumi.Int(0),
					ToPort:     pulumi.Int(0),
					CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				},
			},
			Tags: pulumi.StringMap{
				"Name": pulumi.String(lbSecurityGroup),
			},
		})
		if err != nil {
			return err
		}
		// Add an ingress rule to the security group to allow traffic from the load balancer
		_, err = ec2.NewSecurityGroupRule(ctx, "elb_traffic", &ec2.SecurityGroupRuleArgs{
			Type:                  pulumi.String("ingress"),
			FromPort:              pulumi.Int(80),
			ToPort:                pulumi.Int(80),
			Protocol:              pulumi.String("tcp"),
			SecurityGroupId:       webSecurityGroup.ID(),
			SourceSecurityGroupId: lbSecGroup.ID(),
		})

		//Create a Launch Template
		launchTemplate, err := ec2.NewLaunchTemplate(ctx, "launch-again-take-2", &ec2.LaunchTemplateArgs{
			ImageId:               pulumi.String(amiID),
			InstanceType:          pulumi.String(instanceType),
			KeyName:               pulumi.String("Cloud"),
			DisableApiTermination: pulumi.Bool(false),
			//NetworkInterfaces: ec2.LaunchTemplateNetworkInterfaceArray{
			//	&ec2.LaunchTemplateNetworkInterfaceArgs{
			//		AssociatePublicIpAddress: pulumi.String("true"),
			//		SecurityGroups:           pulumi.StringArray{webSecurityGroup.ID()},
			//	},
			//},
			VpcSecurityGroupIds: pulumi.StringArray{webSecurityGroup.ID()},
			IamInstanceProfile: &ec2.LaunchTemplateIamInstanceProfileArgs{
				Name: instanceProfile.Name,
			},
			//VpcSecurityGroupIds: pulumi.StringArray{webSecurityGroup.ID()},
			UserData: rdsInstance.Endpoint.ApplyT(
				func(args interface{}) (string, error) {
					endpoint := args.(string)
					userData = strings.Replace(userData, "${HOST}", endpoint, -1)
					encodedUserData := base64.StdEncoding.EncodeToString([]byte(userData))
					return encodedUserData, nil
				},
			).(pulumi.StringOutput),
		},
		)
		if err != nil {
			return err
		}
		// Create a Target Group
		tg, err := alb.NewTargetGroup(ctx, "tg", &alb.TargetGroupArgs{
			Port:       pulumi.Int(8080),
			Protocol:   pulumi.String("HTTP"),
			TargetType: pulumi.String("instance"),
			VpcId:      vpc.ID(),
			HealthCheck: &alb.TargetGroupHealthCheckArgs{
				Enabled:  pulumi.Bool(true),
				Interval: pulumi.Int(60),
				Path:     pulumi.String("/healthz"),
				Port:     pulumi.String(strconv.Itoa(8080)),
				Protocol: pulumi.String("HTTP"),
				Timeout:  pulumi.Int(5),
			},
		})
		if err != nil {
			return err
		}

		// Create an Autoscaling Group
		asgGroup, err := autoscaling.NewGroup(ctx, "Auto-Scaling-Group", &autoscaling.GroupArgs{

			MinSize:                pulumi.Int(1),
			MaxSize:                pulumi.Int(3),
			DesiredCapacity:        pulumi.Int(1),
			DefaultCooldown:        pulumi.Int(60),
			HealthCheckType:        pulumi.String("ELB"),
			HealthCheckGracePeriod: pulumi.Int(300),
			LaunchTemplate: &autoscaling.GroupLaunchTemplateArgs{
				Id: launchTemplate.ID(),
			},
			VpcZoneIdentifiers: pulumi.StringArray{publicsubnetIds[0]},
			TargetGroupArns:    pulumi.StringArray{tg.Arn},
		})
		if err != nil {
			return err
		}
		// Create scale up policy
		scaleupPolicy, err := autoscaling.NewPolicy(ctx, "scale-up-policy", &autoscaling.PolicyArgs{
			AdjustmentType:       pulumi.String("ChangeInCapacity"),
			ScalingAdjustment:    pulumi.Int(1),
			PolicyType:           pulumi.String("SimpleScaling"),
			AutoscalingGroupName: asgGroup.Name,
		})
		if err != nil {
			return err
		}

		//Create scale down policy
		scaledownPolicy, err := autoscaling.NewPolicy(ctx, "scale-down-policy", &autoscaling.PolicyArgs{
			AdjustmentType:       pulumi.String("ChangeInCapacity"),
			ScalingAdjustment:    pulumi.Int(-1),
			PolicyType:           pulumi.String("SimpleScaling"),
			AutoscalingGroupName: asgGroup.Name,
		})
		if err != nil {
			return err
		}

		// Create a CloudWatch Alarm
		_, err = cloudwatch.NewMetricAlarm(ctx, "AS-Alarm-scale-up", &cloudwatch.MetricAlarmArgs{
			AlarmDescription:   pulumi.String("Request for the AutoScaling Alarm"),
			EvaluationPeriods:  pulumi.Int(2),
			MetricName:         pulumi.String("CPUUtilization"),
			Namespace:          pulumi.String("AWS/EC2"),
			Period:             pulumi.Int(120),
			Statistic:          pulumi.String("Average"),
			Threshold:          pulumi.Float64(5),
			ComparisonOperator: pulumi.String("GreaterThanOrEqualToThreshold"),
			Dimensions: pulumi.StringMap{
				"AutoScalingGroupName": asgGroup.Name,
			},
			AlarmActions: pulumi.Array{
				scaleupPolicy.Arn,
			},
		})
		if err != nil {
			return err
		}
		// Create a CloudWatch Alarm
		_, err = cloudwatch.NewMetricAlarm(ctx, "AS-Alarm-scale-down", &cloudwatch.MetricAlarmArgs{
			AlarmDescription:   pulumi.String("Request for the AutoScaling Alarm"),
			EvaluationPeriods:  pulumi.Int(2),
			MetricName:         pulumi.String("CPUUtilization"),
			Namespace:          pulumi.String("AWS/EC2"),
			Period:             pulumi.Int(120),
			Statistic:          pulumi.String("Average"),
			Threshold:          pulumi.Float64(3),
			ComparisonOperator: pulumi.String("LessThanOrEqualToThreshold"),
			Dimensions: pulumi.StringMap{
				"AutoScalingGroupName": asgGroup.Name,
			},
			AlarmActions: pulumi.Array{
				scaledownPolicy.Arn,
			},
		})
		if err != nil {
			return err
		}
		//Create a Load Balancer
		lb, err := lb.NewLoadBalancer(ctx, "LoadBalancer", &lb.LoadBalancerArgs{
			Internal:                 pulumi.Bool(false),
			LoadBalancerType:         pulumi.String("application"),
			Subnets:                  pulumi.StringArray{publicsubnetIds[0], publicsubnetIds[1]},
			SecurityGroups:           pulumi.StringArray{lbSecGroup.ID()},
			EnableDeletionProtection: pulumi.Bool(false),
			Tags: pulumi.StringMap{
				"Environment": pulumi.String(environment),
			},
		})
		if err != nil {
			return err
		}

		//Create a Load Balancer Listener
		_, err = alb.NewListener(ctx, "Listener", &alb.ListenerArgs{
			DefaultActions: alb.ListenerDefaultActionArray{
				&alb.ListenerDefaultActionArgs{
					Type:           pulumi.String("forward"),
					TargetGroupArn: tg.Arn,
				},
			},
			LoadBalancerArn: lb.Arn,
			Port:            pulumi.Int(80),
			Protocol:        pulumi.String("HTTP"),
		})

		// Create a new A Record
		_, err = route53.NewRecord(ctx, "A-RECORD", &route53.RecordArgs{
			Name:   pulumi.String(domainName),
			Type:   pulumi.String("A"),
			ZoneId: pulumi.String(zoneID.Id),
			//Records: pulumi.StringArray{lb.Name},
			Aliases: route53.RecordAliasArray{
				&route53.RecordAliasArgs{
					EvaluateTargetHealth: pulumi.Bool(true),
					Name:                 lb.DnsName,
					ZoneId:               lb.ZoneId,
				},
			},
		})
		if err != nil {
			return err
		}

		// Create a SNS Topic
		topic, err := sns.NewTopic(ctx, "userUpdates", &sns.TopicArgs{
			DeliveryPolicy: pulumi.String(`{
    		"http": {
    		"defaultHealthyRetryPolicy": {
      		"minDelayTarget": 20,
      		"maxDelayTarget": 20,
      		"numRetries": 3,
      		"numMaxDelayRetries": 0,
      		"numNoDelayRetries": 0,
      		"numMinDelayRetries": 0,
      		"backoffFunction": "linear"
    		},
    		"disableSubscriptionOverrides": false,
    		"defaultThrottlePolicy": {
      		"maxReceivesPerSecond": 1
    		}
  		}
	}`),
		})
		if err != nil {
			return err
		}
		// Create a DynamoDB Table
		table, err := dynamodb.NewTable(ctx, "Table", &dynamodb.TableArgs{
			Attributes: dynamodb.TableAttributeArray{
				&dynamodb.TableAttributeArgs{
					Name: pulumi.String("Id"),
					Type: pulumi.String("S"),
				},
			},
			HashKey:       pulumi.String("Id"),
			ReadCapacity:  pulumi.Int(5),
			WriteCapacity: pulumi.Int(5),
		})
		if err != nil {
			return err
		}

		if err != nil {
			return err
		}

		topic.Arn.ApplyT(
			func(args interface{}) (string, error) {
				arn := args.(string)
				userData = strings.Replace(userData, "${SUBMISSION_TOPIC_ARN}", arn, -1)
				return arn, nil
			})
		//Create a Google Cloud Storage Bucket
		bucket, err := storage.NewBucket(ctx, "chandana_Bucket", &storage.BucketArgs{
			Location:               pulumi.String("US"),
			Name:                   pulumi.String("chandana-bucket"),
			Project:                pulumi.String("development-406400"),
			StorageClass:           pulumi.String("STANDARD"),
			PublicAccessPrevention: pulumi.String("enforced"),
			ForceDestroy:           pulumi.Bool(true),
		})
		if err != nil {
			return err
		}

		//Create a Service Account for Bucket
		serviceAccount, err := serviceaccount.NewAccount(ctx, "My-Account", &serviceaccount.AccountArgs{

			AccountId:   pulumi.String("service-account-id"),
			DisplayName: pulumi.String("My-Account"),
			Project:     pulumi.String("development-406400"),
		})
		if err != nil {
			return err
		}

		_, err = storage.NewBucketIAMMember(ctx, "bucketIAMMember", &storage.BucketIAMMemberArgs{
			Bucket: bucket.Name,
			Role:   pulumi.String("roles/storage.admin"),
			Member: serviceAccount.Member,
		})
		if err != nil {
			return err
		}
		//Create Access Keys
		AccessKey, err := serviceaccount.NewKey(ctx, "My-Key", &serviceaccount.KeyArgs{
			ServiceAccountId: serviceAccount.Name,
			PublicKeyType:    pulumi.String("TYPE_RAW_PUBLIC_KEY"),
		})
		//Create a Role for Lambda
		lambdaRole, err := iam.NewRole(ctx, "lambdaRole", &iam.RoleArgs{
			AssumeRolePolicy: pulumi.String(`{
				"Version": "2012-10-17",
				"Statement": [
					{
					"Action": "sts:AssumeRole",
					"Principal": {
						"Service": "lambda.amazonaws.com"
					},
					"Effect": "Allow",
					"Sid": ""
					}
				]
				}`),
		})
		if err != nil {
			return err
		}

		// Create a new Lambda Role Policy Attachment
		_, err = iam.NewRolePolicyAttachment(ctx, "lambdaRolePolicyAttachment", &iam.RolePolicyAttachmentArgs{
			Role:      lambdaRole.Name,
			PolicyArn: pulumi.String("arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"),
		})
		if err != nil {
			return err
		}
		_, err = iam.NewRolePolicyAttachment(ctx, "lambdaRolePolicyAttachment-Dynamo", &iam.RolePolicyAttachmentArgs{
			Role:      lambdaRole.Name,
			PolicyArn: pulumi.String("arn:aws:iam::aws:policy/AmazonDynamoDBFullAccess"),
		})
		if err != nil {
			return err
		}

		// Create a new Lambda Function
		function, err := lambda.NewFunction(ctx, "Lambda_Function", &lambda.FunctionArgs{
			Code:    pulumi.NewFileArchive(path),
			Handler: pulumi.String("Handler.lambda_handler"),
			Runtime: pulumi.String("python3.11"),
			Role:    lambdaRole.Arn,
			Timeout: pulumi.IntPtr(10),
			Environment: &lambda.FunctionEnvironmentArgs{
				Variables: pulumi.StringMap{
					"GCP_BUCKET_NAME":     bucket.Name,
					"DYNAMODB_TABLE_NAME": table.Name,
					"GOOGLE_CREDENTIALS":  AccessKey.PrivateKey,
				},
			},
		})

		// Create a Trigger to lambda from SNS
		_, err = lambda.NewPermission(ctx, "lambda_permission", &lambda.PermissionArgs{
			Action:    pulumi.String("lambda:InvokeFunction"),
			Function:  function.Name, // replace `lambda_function` with your Lambda Function resource
			Principal: pulumi.String("sns.amazonaws.com"),
			SourceArn: topic.Arn, // replace `sns_topic` with your SNS Topic resource
		})
		if err != nil {
			return err
		}
		// SNS Topic Subscription
		_, err = sns.NewTopicSubscription(ctx, "lambdaSubscription", &sns.TopicSubscriptionArgs{
			Topic:    topic.Arn,
			Protocol: pulumi.String("lambda"),
			Endpoint: function.Arn,
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
