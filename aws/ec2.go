package awsclient

import (
	"context"
	"fmt"
	"time"

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

// WorkspaceLaunchInput holds the parameters for provisioning a new workspace instance.
type WorkspaceLaunchInput struct {
	AMIID           string
	InstanceType    string
	KeyName         string
	SecurityGroupID string
	SubnetID        string
	NameTag         string // e.g. "workspace-alice"
	UserData        string // base64-encoded cloud-init / shell script (optional)
}

// LaunchInstance starts one new instance from the given AMI and returns its instance ID.
func (c *EC2Client) LaunchInstance(ctx context.Context, in WorkspaceLaunchInput) (string, error) {
	runInput := &ec2.RunInstancesInput{
		ImageId:      aws.String(in.AMIID),
		InstanceType: types.InstanceType(in.InstanceType),
		MinCount:     aws.Int32(1),
		MaxCount:     aws.Int32(1),
		KeyName:      aws.String(in.KeyName),
		NetworkInterfaces: []types.InstanceNetworkInterfaceSpecification{
			{
				DeviceIndex:              aws.Int32(0),
				AssociatePublicIpAddress: aws.Bool(false), // EIP is assigned separately
				SubnetId:                 aws.String(in.SubnetID),
				Groups:                   []string{in.SecurityGroupID},
			},
		},
		TagSpecifications: []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeInstance,
				Tags: []types.Tag{
					{Key: aws.String("Name"), Value: aws.String(in.NameTag)},
					{Key: aws.String("ManagedBy"), Value: aws.String("ec2manager")},
				},
			},
		},
	}
	if in.UserData != "" {
		runInput.UserData = aws.String(in.UserData)
	}
	out, err := c.client.RunInstances(ctx, runInput)
	if err != nil {
		return "", fmt.Errorf("run instances: %w", err)
	}
	if len(out.Instances) == 0 {
		return "", fmt.Errorf("run instances returned no instances")
	}
	return aws.ToString(out.Instances[0].InstanceId), nil
}

// WaitUntilRunning blocks until the instance reaches the "running" state or ctx expires.
func (c *EC2Client) WaitUntilRunning(ctx context.Context, instanceID string) error {
	waiter := ec2.NewInstanceRunningWaiter(c.client)
	return waiter.Wait(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	}, 5*time.Minute)
}

// AllocateAndAssociateEIP allocates a new Elastic IP and associates it with instanceID.
// Returns the allocated public IP address. On association failure the EIP is released
// automatically to avoid leaving orphaned addresses.
func (c *EC2Client) AllocateAndAssociateEIP(ctx context.Context, instanceID string) (string, error) {
	alloc, err := c.client.AllocateAddress(ctx, &ec2.AllocateAddressInput{
		Domain: types.DomainTypeVpc,
	})
	if err != nil {
		return "", fmt.Errorf("allocate EIP: %w", err)
	}

	_, err = c.client.AssociateAddress(ctx, &ec2.AssociateAddressInput{
		InstanceId:   aws.String(instanceID),
		AllocationId: alloc.AllocationId,
	})
	if err != nil {
		_, _ = c.client.ReleaseAddress(ctx, &ec2.ReleaseAddressInput{
			AllocationId: alloc.AllocationId,
		})
		return "", fmt.Errorf("associate EIP %s with instance %s: %w",
			aws.ToString(alloc.AllocationId), instanceID, err)
	}

	return aws.ToString(alloc.PublicIp), nil
}
