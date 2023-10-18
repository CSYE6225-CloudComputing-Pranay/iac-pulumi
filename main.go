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

		conf := config.New(ctx, "")

		vpcCidr := conf.Require("vpcCidr")
		ipv4Cidr := conf.Require("ipv4Cidr")
		ipv6Cidr := conf.Require("ipv6Cidr")

		sshKeyName := conf.Require("sshKeyName")
		instanceType := conf.Require("instanceType")
		rootVolumeSize := conf.RequireInt("rootVolumeSize")
		rootVolumeType := conf.Require("rootVolumeType")

		vpcName, internetGatewayName, publicSubnetName, privateSubnetName, publicRouteTableName, privateRouteTableName, publicRTAName, privateRTAName, securityGroupName, ec2InstanceName := getNameTags(conf)

		amiId := conf.Require("amiId")

		var ports []int
		conf.RequireObject("ports", &ports)

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
		vpc, err := ec2.NewVpc(ctx, vpcName, &ec2.VpcArgs{
			CidrBlock: pulumi.String(vpcCidr),
			Tags: pulumi.StringMap{
				"Name": pulumi.String(vpcName),
			},
		})
		if err != nil {
			return err
		}

		// Create Public Subnets
		publicSubnets := make([]*ec2.Subnet, 0, subnetCount)
		for i := 0; i < subnetCount; i++ {
			publicSubnet, err := ec2.NewSubnet(ctx, publicSubnetName+"-"+strconv.Itoa(i+1), &ec2.SubnetArgs{
				VpcId:               vpc.ID(),
				CidrBlock:           pulumi.String(subnetStrings[i]),
				AvailabilityZone:    pulumi.String(available.Names[i]),
				MapPublicIpOnLaunch: pulumi.Bool(true),
				Tags: pulumi.StringMap{
					"Name": pulumi.String(publicSubnetName + "-" + strconv.Itoa(i+1)),
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
			privateSubnet, err := ec2.NewSubnet(ctx, privateSubnetName+"-"+strconv.Itoa(i+1), &ec2.SubnetArgs{
				VpcId:            vpc.ID(),
				CidrBlock:        pulumi.String(subnetStrings[i+subnetCount]),
				AvailabilityZone: pulumi.String(available.Names[i]),
				Tags: pulumi.StringMap{
					"Name": pulumi.String(privateSubnetName + "-" + strconv.Itoa(i+1)),
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
			_, err := ec2.NewRouteTableAssociation(ctx, publicRTAName+"-"+strconv.Itoa(i+1), &ec2.RouteTableAssociationArgs{
				SubnetId:     subnet.ID(),
				RouteTableId: publicRouteTable.ID(),
			})
			if err != nil {
				return err
			}
		}

		// Associate the Private Subnets to the Private Route Table.
		for i, subnet := range privateSubnets {
			_, err := ec2.NewRouteTableAssociation(ctx, privateRTAName+"-"+strconv.Itoa(i+1), &ec2.RouteTableAssociationArgs{
				SubnetId:     subnet.ID(),
				RouteTableId: privateRouteTable.ID(),
			})
			if err != nil {
				return err
			}
		}

		var securityGroupIngressRules ec2.SecurityGroupIngressArray

		for i := range ports {
			securityGroupIngressRules = append(securityGroupIngressRules, &ec2.SecurityGroupIngressArgs{
				Description:    pulumi.String("TLS from VPC for port " + strconv.Itoa(ports[i])),
				FromPort:       pulumi.Int(ports[i]),
				ToPort:         pulumi.Int(ports[i]),
				Protocol:       pulumi.String("tcp"),
				CidrBlocks:     pulumi.StringArray{pulumi.String(ipv4Cidr)},
				Ipv6CidrBlocks: pulumi.StringArray{pulumi.String(ipv6Cidr)},
			})
		}

		securityGroup, err := ec2.NewSecurityGroup(ctx, securityGroupName, &ec2.SecurityGroupArgs{
			VpcId:   vpc.ID(),
			Ingress: securityGroupIngressRules,
			Tags: pulumi.StringMap{
				"Name": pulumi.String(securityGroupName),
			},
		})
		if err != nil {
			return err
		}

		_, err = ec2.NewInstance(ctx, ec2InstanceName, &ec2.InstanceArgs{

			Ami:                   pulumi.String(amiId),
			SubnetId:              publicSubnets[0].ID(),
			KeyName:               pulumi.String(sshKeyName),
			DisableApiTermination: pulumi.Bool(false),
			InstanceType:          pulumi.String(instanceType),
			RootBlockDevice: &ec2.InstanceRootBlockDeviceArgs{
				VolumeSize: pulumi.Int(rootVolumeSize),
				VolumeType: pulumi.String(rootVolumeType),
			},
			VpcSecurityGroupIds: pulumi.StringArray{securityGroup.ID()},
			Tags: pulumi.StringMap{
				"Name": pulumi.String(ec2InstanceName),
			},
		})
		if err != nil {
			return err
		}

		return err
	})
}

func getNameTags(conf *config.Config) (string, string, string, string, string, string, string, string, string, string) {
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
		securityGroupName = "application security group"
	}
	ec2InstanceName, err := conf.Try("ec2InstanceName")
	if err != nil {
		ec2InstanceName = "assessment application instance"
	}
	return vpcName, internetGatewayName, publicSubnetName, privateSubnetName, publicRouteTableName, privateRouteTableName, publicRTAName, privateRTAName, securityGroupName, ec2InstanceName
}
