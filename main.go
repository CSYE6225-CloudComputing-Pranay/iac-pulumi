package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/c-robinson/iplib"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/acm"
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

type nameTags struct {
	vpcName                         string
	internetGatewayName             string
	publicSubnetName                string
	privateSubnetName               string
	publicRouteTableName            string
	privateRouteTableName           string
	publicRTAName                   string
	privateRTAName                  string
	securityGroupName               string
	databaseSecurityGroupName       string
	databaseSubnetGroupName         string
	databaseParameterGroupName      string
	databaseInstanceName            string
	applicationInstanceName         string
	cloudwatchAgentRoleName         string
	cloudwatchInstanceProfileName   string
	cloudwatchAgentPolicyName       string
	applicationInstanceRecordName   string
	applicationDatabaseEgressName   string
	applicationCloudwatchEgressName string
	loadBalancerSecurityGroupName   string
	targetGroupName                 string
	ec2LaunchTemplateName           string
	loadBalancerName                string
	listenerName                    string
	autoScalingGroupName            string
	scaleUpPolicyName               string
	scaleDownPolicyName             string
	scaleUpAlarmName                string
	scaleDownAlarmName              string
	dynamoDBName                    string
	dynamoDBPolicyName              string
	dynamoDBPolicyAttachmentName    string
	bucketName                      string
	topicName                       string
	lambdaFunctionName              string
	lambdaFunctionPermissionName    string
	serviceAccountName              string
	serviceAccountId                string
	serviceAccountKeyName           string
}

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {

		conf := config.New(ctx, "")

		vpcCidr := conf.Require("vpcCidr")
		ipv4Cidr := conf.Require("ipv4Cidr")
		ipv6Cidr := conf.Require("ipv6Cidr")

		sshKeyName := conf.Require("sshKeyName")
		instanceType := conf.Require("instanceType")

		//Fetching AWS Configuration
		awsConf := config.New(ctx, "aws")

		awsProfile := awsConf.Require("profile")

		//Fetching Database Configuration
		dbConf := config.New(ctx, "database")

		dbFamily := dbConf.Require("family")
		dbStorageSize := dbConf.RequireInt("storageSize")
		dbEngine := dbConf.Require("engine")
		dbEngineVersion := dbConf.Require("engineVersion")
		dbInstanceClass := dbConf.Require("instanceClass")
		dbName := dbConf.Require("name")
		dbMasterUser := dbConf.Require("masterUser")
		dbMasterPassword := dbConf.Require("masterPassword")
		dbPort := dbConf.RequireInt("port")

		//Fetching Application Configuration
		appConf := config.New(ctx, "application")

		appUser := appConf.Require("user")
		appUserGroup := appConf.Require("userGroup")
		appPort := appConf.RequireInt("port")
		appResourceFile := appConf.Require("resourceFile")
		appPropertyFile := appConf.Require("propertyFile")
		appLogFile := appConf.Require("logFile")
		appCloudwatchConfigFile := appConf.Require("cloudwatchConfigFile")
		appBinaryFile := appConf.Require("binaryFile")
		appDomainName := awsProfile + "." + appConf.Require("domainName")
		appHealthCheckPath := appConf.Require("healthCheckPath")
		appGcpProject := appConf.Require("gcpProject")
		path := conf.Require("path")

		var nameTags nameTags

		getNameTags(conf, &nameTags)

		amiId := conf.Require("amiId")

		var ports []int
		conf.RequireObject("ports", &ports)

		var loadBalancerPorts []int
		conf.RequireObject("loadBalancerPorts", &loadBalancerPorts)

		parts := strings.Split(vpcCidr, "/")
		ip := parts[0]
		maskStr := parts[1]
		mask, _ := strconv.Atoi(maskStr)

		n := iplib.NewNet4(net.ParseIP(ip), mask)
		subnets, _ := n.Subnet(24)

		subnetStrings := make([]string, len(subnets))
		for i, subnet := range subnets {
			subnetStrings[i] = subnet.String()
		}

		available, err := aws.GetAvailabilityZones(ctx, &aws.GetAvailabilityZonesArgs{
			State: pulumi.StringRef("available"),
		}, nil)
		if err != nil {
			return err
		}
		azCount := len(available.Names)
		subnetCount := min(azCount, 3)
		// Create a VPC
		vpc, err := ec2.NewVpc(ctx, nameTags.vpcName, &ec2.VpcArgs{
			CidrBlock: pulumi.String(vpcCidr),
			Tags: pulumi.StringMap{
				"Name": pulumi.String(nameTags.vpcName),
			},
		})
		if err != nil {
			return err
		}

		// Create Public Subnets
		publicSubnets := make([]*ec2.Subnet, 0, subnetCount)
		for i := 0; i < subnetCount; i++ {
			publicSubnet, err := ec2.NewSubnet(ctx, nameTags.publicSubnetName+"-"+strconv.Itoa(i+1), &ec2.SubnetArgs{
				VpcId:               vpc.ID(),
				CidrBlock:           pulumi.String(subnetStrings[i]),
				AvailabilityZone:    pulumi.String(available.Names[i]),
				MapPublicIpOnLaunch: pulumi.Bool(true),
				Tags: pulumi.StringMap{
					"Name": pulumi.String(nameTags.publicSubnetName + "-" + strconv.Itoa(i+1)),
				},
			})
			if err != nil {
				return err
			}
			publicSubnets = append(publicSubnets, publicSubnet)
		}

		// Create Private Subnets
		privateSubnets := make([]*ec2.Subnet, 0, subnetCount)
		for i := 0; i < subnetCount; i++ {
			privateSubnet, err := ec2.NewSubnet(ctx, nameTags.privateSubnetName+"-"+strconv.Itoa(i+1), &ec2.SubnetArgs{
				VpcId:            vpc.ID(),
				CidrBlock:        pulumi.String(subnetStrings[i+subnetCount]),
				AvailabilityZone: pulumi.String(available.Names[i]),
				Tags: pulumi.StringMap{
					"Name": pulumi.String(nameTags.privateSubnetName + "-" + strconv.Itoa(i+1)),
				},
			})
			if err != nil {
				return err
			}
			privateSubnets = append(privateSubnets, privateSubnet)
		}

		// Create a Internet gateway
		internetGateway, err := ec2.NewInternetGateway(ctx, nameTags.internetGatewayName, &ec2.InternetGatewayArgs{
			VpcId: vpc.ID(),
			Tags: pulumi.StringMap{
				"Name": pulumi.String(nameTags.internetGatewayName),
			},
		})
		if err != nil {
			return err
		}

		//Create a Public Route Table
		publicRouteTable, err := ec2.NewRouteTable(ctx, nameTags.publicRouteTableName, &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Tags: pulumi.StringMap{
				"Name": pulumi.String(nameTags.publicRouteTableName),
			},
		})
		if err != nil {
			return err
		}

		// Create a Route to the Internet
		_, err = ec2.NewRoute(ctx, "public-route", &ec2.RouteArgs{
			RouteTableId:         publicRouteTable.ID(),
			DestinationCidrBlock: pulumi.String(ipv4Cidr),
			GatewayId:            internetGateway.ID(),
		})
		if err != nil {
			return err
		}

		// Create a Private Route Table
		privateRouteTable, err := ec2.NewRouteTable(ctx, nameTags.privateRouteTableName, &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Tags: pulumi.StringMap{
				"Name": pulumi.String(nameTags.privateRouteTableName),
			},
		})
		if err != nil {
			return err
		}
		// Associate the Public Subnets to the Public Route Table.
		for i, subnet := range publicSubnets {
			_, err := ec2.NewRouteTableAssociation(ctx, nameTags.publicRTAName+"-"+strconv.Itoa(i+1), &ec2.RouteTableAssociationArgs{
				SubnetId:     subnet.ID(),
				RouteTableId: publicRouteTable.ID(),
			})
			if err != nil {
				return err
			}
		}

		// Associate the Private Subnets to the Private Route Table.
		for i, subnet := range privateSubnets {
			_, err := ec2.NewRouteTableAssociation(ctx, nameTags.privateRTAName+"-"+strconv.Itoa(i+1), &ec2.RouteTableAssociationArgs{
				SubnetId:     subnet.ID(),
				RouteTableId: privateRouteTable.ID(),
			})
			if err != nil {
				return err
			}
		}

		// Create ingress rules for the load balancer security group
		var loadBalancerSecurityGroupIngressRules ec2.SecurityGroupIngressArray

		for i := range ports {
			loadBalancerSecurityGroupIngressRules = append(loadBalancerSecurityGroupIngressRules, &ec2.SecurityGroupIngressArgs{
				Description:    pulumi.String("TLS from VPC for port " + strconv.Itoa(loadBalancerPorts[i])),
				FromPort:       pulumi.Int(loadBalancerPorts[i]),
				ToPort:         pulumi.Int(loadBalancerPorts[i]),
				Protocol:       pulumi.String("tcp"),
				CidrBlocks:     pulumi.StringArray{pulumi.String(ipv4Cidr)},
				Ipv6CidrBlocks: pulumi.StringArray{pulumi.String(ipv6Cidr)},
			})
		}

		// Create load balancer security group
		loadBalancerSecurityGroup, err := ec2.NewSecurityGroup(ctx, nameTags.loadBalancerSecurityGroupName, &ec2.SecurityGroupArgs{
			VpcId:   vpc.ID(),
			Ingress: loadBalancerSecurityGroupIngressRules,
			Tags: pulumi.StringMap{
				"Name": pulumi.String(nameTags.loadBalancerSecurityGroupName),
			},
		})
		if err != nil {
			return err
		}

		// Create ingress rules for the application security group
		var securityGroupIngressRules ec2.SecurityGroupIngressArray

		for i := range ports {
			securityGroupIngressRules = append(securityGroupIngressRules, &ec2.SecurityGroupIngressArgs{
				Description: pulumi.String("TLS from VPC for port " + strconv.Itoa(ports[i])),
				FromPort:    pulumi.Int(ports[i]),
				ToPort:      pulumi.Int(ports[i]),
				Protocol:    pulumi.String("tcp"),
				SecurityGroups: pulumi.StringArray{
					loadBalancerSecurityGroup.ID(),
				},
			})
		}
		// Create application security group
		securityGroup, err := ec2.NewSecurityGroup(ctx, nameTags.securityGroupName, &ec2.SecurityGroupArgs{
			VpcId:   vpc.ID(),
			Ingress: securityGroupIngressRules,
			Tags: pulumi.StringMap{
				"Name": pulumi.String(nameTags.securityGroupName),
			},
		})
		if err != nil {
			return err
		}

		// Create database security group
		databaseSecurityGroup, err := ec2.NewSecurityGroup(ctx, nameTags.databaseSecurityGroupName, &ec2.SecurityGroupArgs{
			VpcId: vpc.ID(),
			Ingress: ec2.SecurityGroupIngressArray{
				&ec2.SecurityGroupIngressArgs{
					SecurityGroups: pulumi.StringArray{
						securityGroup.ID(),
					},
					Protocol: pulumi.String("tcp"),
					FromPort: pulumi.Int(dbPort),
					ToPort:   pulumi.Int(dbPort),
				},
			},
			Tags: pulumi.StringMap{
				"Name": pulumi.String(nameTags.databaseSecurityGroupName),
			},
		})
		if err != nil {
			return err
		}

		// Create egress rule for application security group to access database
		_, err = ec2.NewSecurityGroupRule(ctx, nameTags.applicationDatabaseEgressName, &ec2.SecurityGroupRuleArgs{
			Type:                  pulumi.String("egress"),
			FromPort:              pulumi.Int(dbPort),
			ToPort:                pulumi.Int(dbPort),
			Protocol:              pulumi.String("tcp"),
			SecurityGroupId:       securityGroup.ID(),
			SourceSecurityGroupId: databaseSecurityGroup.ID(),
		})
		if err != nil {
			return err
		}

		// Create egress rule for application security group to access cloudwatch
		_, err = ec2.NewSecurityGroupRule(ctx, nameTags.applicationCloudwatchEgressName, &ec2.SecurityGroupRuleArgs{
			Type:            pulumi.String("egress"),
			FromPort:        pulumi.Int(443),
			ToPort:          pulumi.Int(443),
			Protocol:        pulumi.String("tcp"),
			SecurityGroupId: securityGroup.ID(),
			CidrBlocks:      pulumi.StringArray{pulumi.String(ipv4Cidr)},
			Ipv6CidrBlocks:  pulumi.StringArray{pulumi.String(ipv6Cidr)},
		})
		if err != nil {
			return err
		}

		// Create egress rule for load balancer security group to access application
		_, err = ec2.NewSecurityGroupRule(ctx, "loadBalancer-a-egress", &ec2.SecurityGroupRuleArgs{
			Type:                  pulumi.String("egress"),
			FromPort:              pulumi.Int(appPort),
			ToPort:                pulumi.Int(appPort),
			Protocol:              pulumi.String("tcp"),
			SecurityGroupId:       loadBalancerSecurityGroup.ID(),
			SourceSecurityGroupId: securityGroup.ID(),
		})
		if err != nil {
			return err
		}

		// Create a string array to store the subnet ids for the private subnet group
		var privateSubnetIds pulumi.StringArray
		for i := range privateSubnets {
			privateSubnetIds = append(privateSubnetIds, privateSubnets[i].ID())
		}

		// Create a string array to store the subnet ids for the public subnet group
		var publicSubnetIds pulumi.StringArray
		for i := range publicSubnets {
			publicSubnetIds = append(publicSubnetIds, publicSubnets[i].ID())
		}

		// Create a database subnet group
		databaseSubnetGroup, err := rds.NewSubnetGroup(ctx, nameTags.databaseSubnetGroupName, &rds.SubnetGroupArgs{
			SubnetIds: privateSubnetIds,
			Tags: pulumi.StringMap{
				"Name": pulumi.String(nameTags.databaseSubnetGroupName),
			},
		})
		if err != nil {
			return err
		}

		// Create a database parameter group
		databaseParameterGroup, err := rds.NewParameterGroup(ctx, nameTags.databaseParameterGroupName, &rds.ParameterGroupArgs{
			Family: pulumi.String(dbFamily),
			Tags: pulumi.StringMap{
				"Name": pulumi.String(nameTags.databaseParameterGroupName),
			},
		})
		if err != nil {
			return err
		}

		// Create a database instance
		databaseInstance, err := rds.NewInstance(ctx, nameTags.databaseInstanceName, &rds.InstanceArgs{
			AllocatedStorage:    pulumi.Int(dbStorageSize),
			Engine:              pulumi.String(dbEngine),
			EngineVersion:       pulumi.String(dbEngineVersion),
			InstanceClass:       pulumi.String(dbInstanceClass),
			DbName:              pulumi.String(dbName),
			Username:            pulumi.String(dbMasterUser),
			Password:            pulumi.String(dbMasterPassword),
			MultiAz:             pulumi.Bool(false),
			PubliclyAccessible:  pulumi.Bool(false),
			DbSubnetGroupName:   databaseSubnetGroup.Name,
			ParameterGroupName:  databaseParameterGroup.Name,
			VpcSecurityGroupIds: pulumi.StringArray{databaseSecurityGroup.ID()},
			SkipFinalSnapshot:   pulumi.Bool(true),
			Tags: pulumi.StringMap{
				"Name": pulumi.String(nameTags.databaseInstanceName),
			},
		})
		if err != nil {
			return err
		}

		userData := fmt.Sprintf(`#!/bin/bash
{
	echo "DB_HOST=${DB_HOST}"
	echo "DB_PORT=%d"
	echo "DB_USER=%s"
	echo "DB_PASSWORD=%s"
	echo "DB_NAME=%s"
	echo "PORT=%d"
	echo "FILE_PATH=%s"
	echo "LOG_FILE_PATH=%s"
	echo "SUBMISSION_TOPIC_ARN=${SUBMISSION_TOPIC_ARN}"
	echo "AWS_REGION=us-east-1"
} >> %s
sudo chown %s:%s %s
sudo chown %s:%s %s
sudo chown %s:%s %s
sudo chmod 640 %s
{
	sudo /opt/aws/amazon-cloudwatch-agent/bin/amazon-cloudwatch-agent-ctl \
		-a fetch-config \
		-m ec2 \
		-c file:%s \
		-s
}
`, dbPort, dbMasterUser, dbMasterPassword, dbName, appPort, appResourceFile, appLogFile, appPropertyFile, appUser, appUserGroup, appPropertyFile, appUser, appUserGroup, appBinaryFile, appUser, appUserGroup, appResourceFile, appPropertyFile, appCloudwatchConfigFile)

		// Create a Default Role Policy
		policyString, err := json.Marshal(map[string]interface{}{
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
		defaultPolicy := string(policyString)

		// Create a new Role for the cloudwatch agent
		role, err := iam.NewRole(ctx, nameTags.cloudwatchAgentRoleName, &iam.RoleArgs{
			AssumeRolePolicy: pulumi.String(defaultPolicy),
			Tags: pulumi.StringMap{
				"Name": pulumi.String(nameTags.cloudwatchAgentRoleName),
			},
		})
		if err != nil {
			return err
		}

		// Create a new IAM instance profile with cloudwatch agent role.
		instanceProfile, err := iam.NewInstanceProfile(ctx, nameTags.cloudwatchInstanceProfileName, &iam.InstanceProfileArgs{
			Role: role.Name,
			Tags: pulumi.StringMap{
				"Name": pulumi.String(nameTags.cloudwatchInstanceProfileName),
			},
		})
		if err != nil {
			return err
		}

		// Attach the cloud watch agent policy to the cloudwatch role
		_, err = iam.NewRolePolicyAttachment(ctx, nameTags.cloudwatchAgentPolicyName, &iam.RolePolicyAttachmentArgs{
			Role:      role.Name,
			PolicyArn: pulumi.String("arn:aws:iam::aws:policy/CloudWatchAgentServerPolicy"),
		})
		if err != nil {
			return err
		}

		// Attach the SNS policy to the cloudwatch role
		_, err = iam.NewRolePolicyAttachment(ctx, "SNS-policy", &iam.RolePolicyAttachmentArgs{
			Role:      role.Name,
			PolicyArn: pulumi.String("arn:aws:iam::aws:policy/AmazonSNSFullAccess"),
		})
		if err != nil {
			return err
		}

		// Create an ec2 launch template
		ec2LaunchTemplate, err := ec2.NewLaunchTemplate(ctx, nameTags.ec2LaunchTemplateName, &ec2.LaunchTemplateArgs{
			Name:                  pulumi.String(nameTags.ec2LaunchTemplateName),
			ImageId:               pulumi.String(amiId),
			InstanceType:          pulumi.String(instanceType),
			KeyName:               pulumi.String(sshKeyName),
			DisableApiTermination: pulumi.Bool(false),
			VpcSecurityGroupIds:   pulumi.StringArray{securityGroup.ID()},
			UserData: databaseInstance.Address.ApplyT(
				func(args interface{}) (string, error) {
					endpoint := args.(string)
					userData = strings.Replace(userData, "${DB_HOST}", endpoint, -1)
					encodedUserData := base64.StdEncoding.EncodeToString([]byte(userData))
					return encodedUserData, nil
				},
			).(pulumi.StringOutput),
			IamInstanceProfile: &ec2.LaunchTemplateIamInstanceProfileArgs{
				Name: instanceProfile.Name,
			},
			Tags: pulumi.StringMap{
				"Name": pulumi.String(nameTags.ec2LaunchTemplateName),
			},
		},
			pulumi.DependsOn([]pulumi.Resource{databaseInstance}))
		if err != nil {
			return err
		}

		// Create a Target Group
		targetGroup, err := alb.NewTargetGroup(ctx, nameTags.targetGroupName, &alb.TargetGroupArgs{
			Port:       pulumi.Int(appPort),
			Protocol:   pulumi.String("HTTP"),
			TargetType: pulumi.String("instance"),
			VpcId:      vpc.ID(),
			HealthCheck: &alb.TargetGroupHealthCheckArgs{
				Enabled:  pulumi.Bool(true),
				Interval: pulumi.Int(60),
				Path:     pulumi.String(appHealthCheckPath),
				Port:     pulumi.String(strconv.Itoa(appPort)),
				Protocol: pulumi.String("HTTP"),
				Timeout:  pulumi.Int(5),
			},
			Tags: pulumi.StringMap{
				"Name": pulumi.String(nameTags.targetGroupName),
			},
		})
		if err != nil {
			return err
		}

		autoScalingGroup, err := autoscaling.NewGroup(ctx, nameTags.autoScalingGroupName, &autoscaling.GroupArgs{
			Name:                   pulumi.String(nameTags.autoScalingGroupName),
			VpcZoneIdentifiers:     publicSubnetIds,
			DesiredCapacity:        pulumi.Int(1),
			MaxSize:                pulumi.Int(3),
			MinSize:                pulumi.Int(1),
			DefaultCooldown:        pulumi.Int(60),
			HealthCheckType:        pulumi.String("ELB"),
			HealthCheckGracePeriod: pulumi.Int(10),
			LaunchTemplate: &autoscaling.GroupLaunchTemplateArgs{
				Id:      ec2LaunchTemplate.ID(),
				Version: pulumi.String("$Latest"),
			},
			Tags: autoscaling.GroupTagArray{
				&autoscaling.GroupTagArgs{
					Key:               pulumi.String("Name"),
					Value:             pulumi.String(nameTags.applicationInstanceName),
					PropagateAtLaunch: pulumi.Bool(true),
				},
			},
			TargetGroupArns: pulumi.StringArray{targetGroup.Arn},
		})
		if err != nil {
			return err
		}

		// Create scale up policy
		scaleUpPolicy, err := autoscaling.NewPolicy(ctx, nameTags.scaleUpPolicyName, &autoscaling.PolicyArgs{
			AdjustmentType:        pulumi.String("ChangeInCapacity"),
			ScalingAdjustment:     pulumi.Int(1),
			MetricAggregationType: pulumi.String("Average"),
			PolicyType:            pulumi.String("SimpleScaling"),
			AutoscalingGroupName:  autoScalingGroup.Name,
		})
		if err != nil {
			return err
		}

		//Create scale down policy
		scaleDownPolicy, err := autoscaling.NewPolicy(ctx, nameTags.scaleDownPolicyName, &autoscaling.PolicyArgs{
			AdjustmentType:        pulumi.String("ChangeInCapacity"),
			ScalingAdjustment:     pulumi.Int(-1),
			MetricAggregationType: pulumi.String("Average"),
			PolicyType:            pulumi.String("SimpleScaling"),
			AutoscalingGroupName:  autoScalingGroup.Name,
		})
		if err != nil {
			return err
		}

		// Create a CloudWatch Alarm
		_, err = cloudwatch.NewMetricAlarm(ctx, nameTags.scaleUpAlarmName, &cloudwatch.MetricAlarmArgs{
			AlarmDescription:   pulumi.String("Request for the AutoScaling Alarm"),
			EvaluationPeriods:  pulumi.Int(2),
			MetricName:         pulumi.String("CPUUtilization"),
			Namespace:          pulumi.String("AWS/EC2"),
			Period:             pulumi.Int(120),
			Statistic:          pulumi.String("Average"),
			Threshold:          pulumi.Float64(5),
			ComparisonOperator: pulumi.String("GreaterThanThreshold"),
			Dimensions: pulumi.StringMap{
				"AutoScalingGroupName": autoScalingGroup.Name,
			},
			AlarmActions: pulumi.Array{
				scaleUpPolicy.Arn,
			},
		})
		if err != nil {
			return err
		}

		// Create a CloudWatch Alarm
		_, err = cloudwatch.NewMetricAlarm(ctx, nameTags.scaleDownAlarmName, &cloudwatch.MetricAlarmArgs{
			AlarmDescription:   pulumi.String("Request for the AutoScaling Alarm"),
			EvaluationPeriods:  pulumi.Int(2),
			MetricName:         pulumi.String("CPUUtilization"),
			Namespace:          pulumi.String("AWS/EC2"),
			Period:             pulumi.Int(120),
			Statistic:          pulumi.String("Average"),
			Threshold:          pulumi.Float64(3),
			ComparisonOperator: pulumi.String("LessThanThreshold"),
			Dimensions: pulumi.StringMap{
				"AutoScalingGroupName": autoScalingGroup.Name,
			},
			AlarmActions: pulumi.Array{
				scaleDownPolicy.Arn,
			},
		})
		if err != nil {
			return err
		}

		//Create a Load Balancer
		loadBalancer, err := lb.NewLoadBalancer(ctx, nameTags.loadBalancerName, &lb.LoadBalancerArgs{
			Internal:                 pulumi.Bool(false),
			LoadBalancerType:         pulumi.String("application"),
			Subnets:                  publicSubnetIds,
			SecurityGroups:           pulumi.StringArray{loadBalancerSecurityGroup.ID()},
			EnableDeletionProtection: pulumi.Bool(false),
			Tags: pulumi.StringMap{
				"Name": pulumi.String(nameTags.loadBalancerName),
			},
		})

		// Lookup for the certificate
		certificate, err := acm.LookupCertificate(ctx, &acm.LookupCertificateArgs{
			Domain: appDomainName,
			Statuses: []string{
				"ISSUED",
			},
		})

		if err != nil {
			return err
		}
		//Create a Load Balancer Listener
		_, err = alb.NewListener(ctx, nameTags.listenerName, &alb.ListenerArgs{
			DefaultActions: alb.ListenerDefaultActionArray{
				&alb.ListenerDefaultActionArgs{
					Type:           pulumi.String("forward"),
					TargetGroupArn: targetGroup.Arn,
				},
			},
			LoadBalancerArn: loadBalancer.Arn,
			CertificateArn:  pulumi.String(certificate.Arn),
			Port:            pulumi.Int(443),
			Protocol:        pulumi.String("HTTPS"),
			Tags: pulumi.StringMap{
				"Name": pulumi.String(nameTags.listenerName),
			},
		}, pulumi.DependsOn([]pulumi.Resource{loadBalancer}))
		if err != nil {
			return err
		}

		// Get the zone for application domain
		zoneID, err := route53.LookupZone(ctx, &route53.LookupZoneArgs{
			Name: pulumi.StringRef(appDomainName),
		}, nil)

		if err != nil {
			return err
		}

		// Create a new A Record for the ec2 instance
		_, err = route53.NewRecord(ctx, nameTags.applicationInstanceRecordName, &route53.RecordArgs{
			Name:   pulumi.String(appDomainName),
			Type:   pulumi.String("A"),
			ZoneId: pulumi.String(zoneID.Id),
			Aliases: route53.RecordAliasArray{
				&route53.RecordAliasArgs{
					EvaluateTargetHealth: pulumi.Bool(true),
					Name:                 loadBalancer.DnsName,
					ZoneId:               loadBalancer.ZoneId,
				},
			},
			AllowOverwrite: pulumi.Bool(true)},
			pulumi.DependsOn([]pulumi.Resource{loadBalancer}))
		if err != nil {
			return err
		}

		// Create a SNS Topic
		topic, err := sns.NewTopic(ctx, nameTags.topicName, &sns.TopicArgs{})
		if err != nil {
			return err
		}
		topic.Arn.ApplyT(
			func(args interface{}) (string, error) {
				arn := args.(string)
				userData = strings.Replace(userData, "${SUBMISSION_TOPIC_ARN}", arn, -1)
				return arn, nil
			})

		// Create a DynamoDB Table
		table, err := dynamodb.NewTable(ctx, nameTags.dynamoDBName, &dynamodb.TableArgs{
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
		//Create a Google Cloud Storage Bucket
		bucket, err := storage.NewBucket(ctx, nameTags.bucketName, &storage.BucketArgs{
			Location:               pulumi.String("US"),
			Name:                   pulumi.String(nameTags.bucketName),
			Project:                pulumi.String(appGcpProject),
			StorageClass:           pulumi.String("STANDARD"),
			PublicAccessPrevention: pulumi.String("enforced"),
		})
		if err != nil {
			return err
		}

		//Create a Service Account for Bucket
		serviceAccount, err := serviceaccount.NewAccount(ctx, nameTags.serviceAccountName, &serviceaccount.AccountArgs{
			AccountId:   pulumi.String(nameTags.serviceAccountId),
			DisplayName: pulumi.String(nameTags.serviceAccountName),
			Project:     pulumi.String(appGcpProject),
		})
		if err != nil {
			return err
		}
		//Create Access Keys
		accessKey, err := serviceaccount.NewKey(ctx, nameTags.serviceAccountKeyName, &serviceaccount.KeyArgs{
			ServiceAccountId: serviceAccount.Name,
			PublicKeyType:    pulumi.String("TYPE_X509_PEM_FILE"),
		})

		// Create Access Grant for the Bucket to the Service Account
		_, err = storage.NewBucketIAMMember(ctx, "My-Bucket-Binding", &storage.BucketIAMMemberArgs{
			Bucket: bucket.Name,
			Role:   pulumi.String("roles/storage.admin"),
			Member: serviceAccount.Member,
		})
		if err != nil {
			return err
		}
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

		dynamodbPolicyDocument, err := iam.GetPolicyDocument(ctx, &iam.GetPolicyDocumentArgs{
			Statements: []iam.GetPolicyDocumentStatement{
				{
					Effect: pulumi.StringRef("Allow"),
					Actions: []string{
						"dynamodb:GetItem",
						"dynamodb:PutItem",
						"dynamodb:UpdateItem",
						"dynamodb:DeleteItem",
						"dynamodb:Scan",
						"dynamodb:Query",
					},
					Resources: []string{
						"arn:aws:dynamodb:*:*:table/*",
					},
				},
			},
		}, nil)
		if err != nil {
			return err
		}

		dynamodbPolicy, err := iam.NewPolicy(ctx, nameTags.dynamoDBPolicyName, &iam.PolicyArgs{
			Path:        pulumi.String("/"),
			Description: pulumi.String("IAM policy for dynamodb"),
			Policy:      pulumi.String(dynamodbPolicyDocument.Json),
		})
		if err != nil {
			return err
		}

		_, err = iam.NewRolePolicyAttachment(ctx, nameTags.dynamoDBPolicyAttachmentName, &iam.RolePolicyAttachmentArgs{
			Role:      lambdaRole.Name,
			PolicyArn: dynamodbPolicy.Arn,
		}, pulumi.DependsOn([]pulumi.Resource{
			lambdaRole,
			dynamodbPolicy,
		}))
		if err != nil {
			return err
		}

		// Create a new Lambda Function
		function, err := lambda.NewFunction(ctx, nameTags.lambdaFunctionName, &lambda.FunctionArgs{
			Code:    pulumi.NewFileArchive(path),
			Handler: pulumi.String("lambda.lambda_handler"),
			Runtime: pulumi.String("python3.11"),
			Role:    lambdaRole.Arn,
			Timeout: pulumi.Int(15),
			Environment: &lambda.FunctionEnvironmentArgs{
				Variables: pulumi.StringMap{
					"GOOGLE_CREDENTIALS": accessKey.PrivateKey,
					"FROM_ADDRESS":       "mailgun@" + pulumi.String(appDomainName),
					"GCP_BUCKET_NAME":    bucket.Name,
					"DYNAMO_TABLE_NAME":  table.Name,
				},
			},
		})

		// Create a Trigger to lambda from SNS
		_, err = lambda.NewPermission(ctx, nameTags.lambdaFunctionPermissionName, &lambda.PermissionArgs{
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

		ctx.Export("Database Endpoint", databaseInstance.Endpoint)

		return err
	})
}

func getNameTags(conf *config.Config, nameTags *nameTags) {
	vpcName, err := conf.Try("vpcName")
	if err != nil {
		vpcName = "my-vpc"
	}
	internetGatewayName, err := conf.Try("internetGatewayName")
	if err != nil {
		internetGatewayName = "Internet-Gateway"
	}
	publicSubnetName, err := conf.Try("publicSubnetName")
	if err != nil {
		publicSubnetName = "public-subnet"
	}
	privateSubnetName, err := conf.Try("privateSubnetName")
	if err != nil {
		privateSubnetName = "private-subnet"
	}
	publicRouteTableName, err := conf.Try("publicRouteTableName")
	if err != nil {
		publicRouteTableName = "public-route-table"
	}
	privateRouteTableName, err := conf.Try("privateRouteTableName")
	if err != nil {
		privateRouteTableName = "private-route-table"
	}
	publicRTAName, err := conf.Try("publicRTAName")
	if err != nil {
		publicRTAName = "publicRTA"
	}
	privateRTAName, err := conf.Try("privateRTAName")
	if err != nil {
		privateRTAName = "privateRTA"
	}
	securityGroupName, err := conf.Try("securityGroupName")
	if err != nil {
		securityGroupName = "application-security-group"
	}
	databaseSecurityGroupName, err := conf.Try("databaseSecurityGroupName")
	if err != nil {
		databaseSecurityGroupName = "database-security-group"
	}
	databaseSubnetGroupName, err := conf.Try("databaseSubnetGroupName")
	if err != nil {
		databaseSubnetGroupName = "database-subnet-group"
	}
	databaseParameterGroupName, err := conf.Try("databaseParameterGroupName")
	if err != nil {
		databaseParameterGroupName = "database-parameter-group"
	}
	databaseInstanceName, err := conf.Try("databaseInstanceName")
	if err != nil {
		databaseInstanceName = "assessment-application-database"
	}
	applicationInstanceName, err := conf.Try("applicationInstanceName")
	if err != nil {
		applicationInstanceName = "assessment-application-instance"
	}
	cloudwatchAgentRoleName, err := conf.Try("cloudwatchAgentRoleName")
	if err != nil {
		cloudwatchAgentRoleName = "cloudwatch-agent-role"
	}
	cloudwatchInstanceProfileName, err := conf.Try("cloudwatchInstanceProfileName")
	if err != nil {
		cloudwatchInstanceProfileName = "cloudwatch-instance-profile"
	}
	cloudwatchAgentPolicyName, err := conf.Try("cloudwatchAgentPolicyName")
	if err != nil {
		cloudwatchAgentPolicyName = "cloudwatch-agent-policy"
	}
	applicationInstanceRecordName, err := conf.Try("applicationInstanceRecordName")
	if err != nil {
		applicationInstanceRecordName = "application-instance-record"
	}
	applicationDatabaseEgressName, err := conf.Try("applicationDatabaseEgressName")
	if err != nil {
		applicationDatabaseEgressName = "application-database-egress"
	}
	applicationCloudwatchEgressName, err := conf.Try("applicationCloudwatchEgressName")
	if err != nil {
		applicationCloudwatchEgressName = "application-cloudwatch-egress"
	}
	loadBalancerSecurityGroupName, err := conf.Try("loadBalancerSecurityGroupName")
	if err != nil {
		loadBalancerSecurityGroupName = "load-balancer-security-group"
	}
	ec2LaunchTemplateName, err := conf.Try("ec2LaunchTemplateName")
	if err != nil {
		ec2LaunchTemplateName = "csye6225_asg"
	}
	loadBalancerName, err := conf.Try("loadBalancerName")
	if err != nil {
		loadBalancerName = "load-balancer"
	}
	listenerName, err := conf.Try("listenerName")
	if err != nil {
		listenerName = "listener"
	}
	autoScalingGroupName, err := conf.Try("autoScalingGroupName")
	if err != nil {
		autoScalingGroupName = "auto-scaling-group"
	}
	scaleUpPolicyName, err := conf.Try("scaleUpPolicyName")
	if err != nil {
		scaleUpPolicyName = "scale-up-policy"
	}
	scaleDownPolicyName, err := conf.Try("scaleDownPolicyName")
	if err != nil {
		scaleDownPolicyName = "scale-down-policy"
	}
	scaleUpAlarmName, err := conf.Try("scaleUpAlarmName")
	if err != nil {
		scaleUpAlarmName = "scale-up-alarm"
	}
	scaleDownAlarmName, err := conf.Try("scaleDownAlarmName")
	if err != nil {
		scaleDownAlarmName = "scale-down-alarm"
	}
	targetGroupName, err := conf.Try("targetGroupName")
	if err != nil {
		targetGroupName = "target-group"
	}
	dynamoDBName, err := conf.Try("dynamoDBName")
	if err != nil {
		dynamoDBName = "Submission-table"
	}
	bucketName, err := conf.Try("bucketName")
	if err != nil {
		bucketName = "pranay-bucket-csye6225"
	}
	topicName, err := conf.Try("topicName")
	if err != nil {
		topicName = "assessment-application-topic"
	}
	lambdaFunctionName, err := conf.Try("lambdaFunctionName")
	if err != nil {
		lambdaFunctionName = "assessment-application-lambda"
	}
	dynamoDBPolicyName, err := conf.Try("dynamoDBPolicyName")
	if err != nil {
		dynamoDBPolicyName = "dynamodb-policy"
	}
	dynamoDBPolicyAttachmentName, err := conf.Try("dynamoDBPolicyAttachmentName")
	if err != nil {
		dynamoDBPolicyAttachmentName = "dynamodb-policy-attachment"
	}
	lambdaFunctionPermissionName, err := conf.Try("lambdaFunctionPermissionName")
	if err != nil {
		lambdaFunctionPermissionName = "lambda-function-permission"
	}
	serviceAccountName, err := conf.Try("serviceAccountName")
	if err != nil {
		serviceAccountName = "assessment-application-service-account"
	}
	serviceAccountId, err := conf.Try("serviceAccountId")
	if err != nil {
		serviceAccountId = "service-account-id"
	}
	serviceAccountKeyName, err := conf.Try("serviceAccountKeyName")
	if err != nil {
		serviceAccountKeyName = "assessment-application-service-account-key"
	}
	nameTags.vpcName = vpcName
	nameTags.internetGatewayName = internetGatewayName
	nameTags.publicSubnetName = publicSubnetName
	nameTags.privateSubnetName = privateSubnetName
	nameTags.publicRouteTableName = publicRouteTableName
	nameTags.privateRouteTableName = privateRouteTableName
	nameTags.publicRTAName = publicRTAName
	nameTags.privateRTAName = privateRTAName
	nameTags.securityGroupName = securityGroupName
	nameTags.databaseSecurityGroupName = databaseSecurityGroupName
	nameTags.databaseSubnetGroupName = databaseSubnetGroupName
	nameTags.databaseParameterGroupName = databaseParameterGroupName
	nameTags.databaseInstanceName = databaseInstanceName
	nameTags.applicationInstanceName = applicationInstanceName
	nameTags.cloudwatchAgentRoleName = cloudwatchAgentRoleName
	nameTags.cloudwatchInstanceProfileName = cloudwatchInstanceProfileName
	nameTags.cloudwatchAgentPolicyName = cloudwatchAgentPolicyName
	nameTags.applicationInstanceRecordName = applicationInstanceRecordName
	nameTags.applicationDatabaseEgressName = applicationDatabaseEgressName
	nameTags.applicationCloudwatchEgressName = applicationCloudwatchEgressName
	nameTags.loadBalancerSecurityGroupName = loadBalancerSecurityGroupName
	nameTags.ec2LaunchTemplateName = ec2LaunchTemplateName
	nameTags.loadBalancerName = loadBalancerName
	nameTags.listenerName = listenerName
	nameTags.autoScalingGroupName = autoScalingGroupName
	nameTags.scaleUpPolicyName = scaleUpPolicyName
	nameTags.scaleDownPolicyName = scaleDownPolicyName
	nameTags.scaleUpAlarmName = scaleUpAlarmName
	nameTags.scaleDownAlarmName = scaleDownAlarmName
	nameTags.targetGroupName = targetGroupName
	nameTags.dynamoDBName = dynamoDBName
	nameTags.bucketName = bucketName
	nameTags.topicName = topicName
	nameTags.lambdaFunctionName = lambdaFunctionName
	nameTags.dynamoDBPolicyName = dynamoDBPolicyName
	nameTags.dynamoDBPolicyAttachmentName = dynamoDBPolicyAttachmentName
	nameTags.lambdaFunctionPermissionName = lambdaFunctionPermissionName
	nameTags.serviceAccountName = serviceAccountName
	nameTags.serviceAccountId = serviceAccountId
	nameTags.serviceAccountKeyName = serviceAccountKeyName
	return
}
