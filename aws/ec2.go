package awsclient

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// EC2Client wraps the AWS EC2 service client.
// Credentials are loaded automatically from the IAM role attached to this EC2
// instance (via IMDS), environment variables, or ~/.aws/credentials – in that
// order. No credentials are ever hardcoded.
type EC2Client struct {
	client *ec2.Client
}

// InstanceInfo holds the fields we surface in the UI.
type InstanceInfo struct {
	InstanceID   string
	State        string
	PublicIP     string
	InstanceType string
}

// NewEC2Client creates a new client using the default credential chain.
func NewEC2Client(ctx context.Context, region string) (*EC2Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return &EC2Client{client: ec2.NewFromConfig(cfg)}, nil
}

// StartInstance sends a StartInstances request for the given instance.
func (c *EC2Client) StartInstance(ctx context.Context, instanceID string) error {
	_, err := c.client.StartInstances(ctx, &ec2.StartInstancesInput{
		InstanceIds: []string{instanceID},
	})
	return err
}

// StopInstance sends a StopInstances request for the given instance.
func (c *EC2Client) StopInstance(ctx context.Context, instanceID string) error {
	_, err := c.client.StopInstances(ctx, &ec2.StopInstancesInput{
		InstanceIds: []string{instanceID},
	})
	return err
}

// DescribeInstance retrieves current state and IP for a single instance.
func (c *EC2Client) DescribeInstance(ctx context.Context, instanceID string) (*InstanceInfo, error) {
	out, err := c.client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		return nil, fmt.Errorf("describe instance %s: %w", instanceID, err)
	}

	for _, r := range out.Reservations {
		for _, inst := range r.Instances {
			info := &InstanceInfo{
				InstanceID:   aws.ToString(inst.InstanceId),
				InstanceType: string(inst.InstanceType),
			}
			if inst.State != nil {
				info.State = string(inst.State.Name)
			}
			if inst.PublicIpAddress != nil {
				info.PublicIP = aws.ToString(inst.PublicIpAddress)
			}
			return info, nil
		}
	}
	return nil, fmt.Errorf("instance %s not found", instanceID)
}

// IsRunning returns true when the instance is in the "running" state.
func (c *EC2Client) IsRunning(ctx context.Context, instanceID string) (bool, error) {
	info, err := c.DescribeInstance(ctx, instanceID)
	if err != nil {
		return false, err
	}
	return info.State == string(types.InstanceStateNameRunning), nil
}

// IsStopped returns true when the instance is in the "stopped" state.
func (c *EC2Client) IsStopped(ctx context.Context, instanceID string) (bool, error) {
	info, err := c.DescribeInstance(ctx, instanceID)
	if err != nil {
		return false, err
	}
	return info.State == string(types.InstanceStateNameStopped), nil
}
