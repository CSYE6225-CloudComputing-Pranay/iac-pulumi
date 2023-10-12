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

		cidrBlock := conf.Require("cidrBlock")
		vpcName := conf.Require("vpcName")
		destinationBlock := conf.Require("destinationBlock")

		internetGatewayName := conf.Require("internetGatewayName")
		publicSubnetName := conf.Require("publicSubnetName")
		privateSubnetName := conf.Require("privateSubnetName")
		publicRouteTableName := conf.Require("publicRouteTableName")
		privateRouteTableName := conf.Require("privateRouteTableName")
		publicRTAName := conf.Require("publicRTAName")
		privateRTAName := conf.Require("privateRTAName")

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
			CidrBlock: pulumi.String(cidrBlock),
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
			DestinationCidrBlock: pulumi.String(destinationBlock),
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
		return err
	})
}
