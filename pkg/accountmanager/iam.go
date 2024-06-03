package accountmanager

import (
	"fmt"
	"log"

	"github.com/Optum/dce/pkg/account"
	"github.com/Optum/dce/pkg/common"
	"github.com/Optum/dce/pkg/errors"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
)

type principalService struct {
	iamSvc   iamiface.IAMAPI
	storager common.Storager
	account  *account.Account
	config   ServiceConfig
}

func (p *principalService) MergeRole() error {

	_, err := p.iamSvc.CreateRole(&iam.CreateRoleInput{
		RoleName:                 p.account.PrincipalRoleArn.IAMResourceName(),
		AssumeRolePolicyDocument: aws.String(p.config.assumeRolePolicy),
		Description:              aws.String(p.config.PrincipalRoleDescription),
		MaxSessionDuration:       aws.Int64(p.config.PrincipalMaxSessionDuration),
		Tags: append(p.config.tags,
			&iam.Tag{Key: aws.String("Name"), Value: aws.String("DCEPrincipal")},
		),
	})
	if err != nil {
		if isAWSAlreadyExistsError(err) {
			log.Printf("%s: for account %q; ignoring", err.Error(), *p.account.ID)
		} else {
			return errors.NewInternalServer(fmt.Sprintf("unexpected error creating role %q", p.account.PrincipalRoleArn.String()), err)
		}
	}

	return nil
}

func (p *principalService) DeleteRole() error {

	_, err := p.iamSvc.DeleteRole(&iam.DeleteRoleInput{
		RoleName: p.account.PrincipalRoleArn.IAMResourceName(),
	})
	if err != nil {
		if isAWSNoSuchEntityError(err) {
			log.Printf("%s: for account %q; ignoring", err.Error(), *p.account.ID)
		} else {
			return errors.NewInternalServer(fmt.Sprintf("unexpected error deleting the role %q", p.account.PrincipalRoleArn.String()), err)
		}
	}

	return nil
}

func (p *principalService) MergePolicy() error {

	policy, policyHash, err := p.buildPolicy()
	if err != nil {
		return err
	}

	// if they match there is nothing to do
	// Added account ID to log messages to help troubleshoot which account is having error with updating principal policy.
	if p.account.PrincipalPolicyHash != nil {
		if *policyHash == *p.account.PrincipalPolicyHash {
			log.Printf("SKIP: For account %q, Policy Hash matches.  Old %q and New %q", *p.account.ID, *p.account.PrincipalPolicyHash, *policyHash)
			return nil
		}
		log.Printf("UPDATE: For account %q, Policy Hash doesn't match.  Old %q and New %q", *p.account.ID, *p.account.PrincipalPolicyHash, *policyHash)
	} else {
		log.Printf("UPDATE: For account %q, Old Policy Hash is null. New %q", *p.account.ID, *policyHash)
	}

	_, err = p.iamSvc.CreatePolicy(&iam.CreatePolicyInput{
		PolicyName:     p.account.PrincipalPolicyArn.IAMResourceName(),
		Description:    aws.String(p.config.PrincipalPolicyDescription),
		PolicyDocument: policy,
	})

	if err != nil {
		if isAWSAlreadyExistsError(err) {
			log.Printf("%s: for account %q; ignoring", err.Error(), *p.account.ID)
		} else {
			return errors.NewInternalServer(fmt.Sprintf("unexpected error creating policy %q", p.account.PrincipalPolicyArn.String()), err)
		}
	} else {
		// no error means we create the policy without issue moving on
		p.account.PrincipalPolicyHash = policyHash
		return nil
	}

	// Prune old versions of the policy.  Making sure we have room for one more policy version
	err = p.prunePolicyVersions()
	if err != nil {
		return err
	}

	// Create a new Policy Version and set as default
	_, err = p.iamSvc.CreatePolicyVersion(&iam.CreatePolicyVersionInput{
		PolicyArn:      aws.String(p.account.PrincipalPolicyArn.String()),
		PolicyDocument: policy,
		SetAsDefault:   aws.Bool(true),
	})

	p.account.PrincipalPolicyHash = policyHash
	if err != nil {
		return errors.NewInternalServer(fmt.Sprintf("unexpected error creating policy version %q", p.account.PrincipalPolicyArn.String()), err)
	}

	return nil
}

func (p *principalService) DeletePolicy() error {

	versions, err := p.iamSvc.ListPolicyVersions(&iam.ListPolicyVersionsInput{
		PolicyArn: aws.String(p.account.PrincipalPolicyArn.String()),
	})
	if err != nil {
		return errors.NewInternalServer(fmt.Sprintf("unexpected error listing policy versions on %q", p.account.PrincipalPolicyArn.String()), err)
	}
	for _, version := range versions.Versions {
		if !*version.IsDefaultVersion {
			err = p.deletePolicyVersion(version)
			if err != nil {
				return err
			}
		}
	}

	_, err = p.iamSvc.DeletePolicy(&iam.DeletePolicyInput{
		PolicyArn: aws.String(p.account.PrincipalPolicyArn.String()),
	})

	if err != nil {
		if isAWSNoSuchEntityError(err) {
			log.Printf("%s: for account %q; ignoring", err.Error(), *p.account.ID)
		} else {
			return errors.NewInternalServer(fmt.Sprintf("unexpected error deleting the policy %q", p.account.PrincipalPolicyArn.String()), err)
		}
	}

	return nil
}

func (p *principalService) AttachRoleWithPolicy() error {

	// Attach the policy to the role
	_, err := p.iamSvc.AttachRolePolicy(&iam.AttachRolePolicyInput{
		PolicyArn: aws.String(p.account.PrincipalPolicyArn.String()),
		RoleName:  p.account.PrincipalRoleArn.IAMResourceName(),
	})
	if err != nil {
		if isAWSAlreadyExistsError(err) {
			log.Printf("%s: for account %q; ignoring", err.Error(), *p.account.ID)
		} else {
			return errors.NewInternalServer(
				fmt.Sprintf("unexpected error attaching policy %q to role %q", p.account.PrincipalPolicyArn.String(), p.account.PrincipalRoleArn.String()),
				err)
		}
	}

	return nil
}

func (p *principalService) DetachRoleWithPolicy() error {

	// Detach the policy to the role
	_, err := p.iamSvc.DetachRolePolicy(&iam.DetachRolePolicyInput{
		PolicyArn: aws.String(p.account.PrincipalPolicyArn.String()),
		RoleName:  p.account.PrincipalRoleArn.IAMResourceName(),
	})
	if err != nil {
		if isAWSNoSuchEntityError(err) {
			log.Printf("%s: for account %q; ignoring", err.Error(), *p.account.ID)
		} else {
			return errors.NewInternalServer(
				fmt.Sprintf("unexpected error detaching policy %q from role %q", p.account.PrincipalPolicyArn.String(), p.account.PrincipalRoleArn.String()),
				err)
		}
	}

	return nil
}

func (p *principalService) buildPolicy() (*string, *string, error) {

	type principalPolicyInput struct {
		PrincipalPolicyArn   string
		PrincipalRoleArn     string
		PrincipalIAMDenyTags []string
		AdminRoleArn         string
		Regions              []string
	}

	policy, policyHash, err := p.storager.GetTemplateObject(p.config.S3BucketName, p.config.S3PolicyKey,
		principalPolicyInput{
			PrincipalPolicyArn:   p.account.PrincipalPolicyArn.String(),
			PrincipalRoleArn:     p.account.PrincipalRoleArn.String(),
			PrincipalIAMDenyTags: p.config.PrincipalIAMDenyTags,
			AdminRoleArn:         p.account.AdminRoleArn.String(),
			Regions:              p.config.AllowedRegions,
		})
	if err != nil {
		return nil, nil, err
	}

	return &policy, &policyHash, nil
}

// PrunePolicyVersions to prune the oldest version if at 5 versions
func (p *principalService) prunePolicyVersions() error {
	versions, err := p.iamSvc.ListPolicyVersions(&iam.ListPolicyVersionsInput{
		PolicyArn: aws.String(p.account.PrincipalPolicyArn.String()),
	})
	if err != nil {
		return errors.NewInternalServer(fmt.Sprintf("unexpected error listing policy versions on %q", p.account.PrincipalPolicyArn.String()), err)
	}
	if len(versions.Versions) < 5 && len(versions.Versions) > 1 {
		return nil
	}

	var oldestVersion *iam.PolicyVersion

	for _, version := range versions.Versions {
		if *version.IsDefaultVersion {
			continue
		}
		if oldestVersion == nil ||
			version.CreateDate.Before(*oldestVersion.CreateDate) {
			oldestVersion = version
		}

	}

	if oldestVersion != nil {
		return p.deletePolicyVersion(oldestVersion)
	}

	return nil
}

// DeletePolicyVersion delete a version of a template
func (p *principalService) deletePolicyVersion(version *iam.PolicyVersion) error {
	request := &iam.DeletePolicyVersionInput{
		PolicyArn: aws.String(p.account.PrincipalPolicyArn.String()),
		VersionId: version.VersionId,
	}

	_, err := p.iamSvc.DeletePolicyVersion(request)
	if err != nil {
		return errors.NewInternalServer(
			fmt.Sprintf("unexpected error deleting policy version %q on policy %q", *version.VersionId, p.account.PrincipalPolicyArn.String()),
			err,
		)
	}
	return nil
}

// bluepi added functionalities

func (p *principalService) MergeRoleBluepi(role_name string) error {
	log.Printf("MergeRoleBluepi")
	_, err := p.iamSvc.CreateRole(&iam.CreateRoleInput{
		RoleName:                 aws.String(role_name),
		AssumeRolePolicyDocument: aws.String(p.config.assumeRolePolicy),
		Description:              aws.String(p.config.PrincipalRoleDescription),
		MaxSessionDuration:       aws.Int64(p.config.PrincipalMaxSessionDuration),
		Tags: append(p.config.tags,
			&iam.Tag{Key: aws.String("Name"), Value: aws.String("DCEPrincipal")},
		),
	})
	if err != nil {
		if isAWSAlreadyExistsError(err) {
			log.Printf("%s: for account %q; ignoring", err.Error(), *p.account.ID)
		} else {
			return errors.NewInternalServer(fmt.Sprintf("unexpected error creating role %q", role_name), err)
		}
	}

	return nil
}

func (p *principalService) MergePolicyBluepi(policy_name string, role_name string) error {
	log.Printf("MergePolicyBluepi")

	policy, _, err := p.buildPolicyBluepi(policy_name, role_name)
	if err != nil {
		log.Printf("Error in Building policy : %s", err)
		return err
	}

	// // if they match there is nothing to do
	// // Added account ID to log messages to help troubleshoot which account is having error with updating principal policy.
	// if p.account.PrincipalPolicyHash != nil {
	// 	if *policyHash == *p.account.PrincipalPolicyHash {
	// 		log.Printf("SKIP: For account %q, Policy Hash matches.  Old %q and New %q", *p.account.ID, *p.account.PrincipalPolicyHash, *policyHash)
	// 		return nil
	// 	}
	// 	log.Printf("UPDATE: For account %q, Policy Hash doesn't match.  Old %q and New %q", *p.account.ID, *p.account.PrincipalPolicyHash, *policyHash)
	// } else {
	// 	log.Printf("UPDATE: For account %q, Old Policy Hash is null. New %q", *p.account.ID, *policyHash)
	// }

	_, err = p.iamSvc.CreatePolicy(&iam.CreatePolicyInput{
		PolicyName:     aws.String(policy_name),
		Description:    aws.String(p.config.PrincipalPolicyDescription),
		PolicyDocument: policy,
	})

	if err != nil {
		if isAWSAlreadyExistsError(err) {
			log.Printf("%s: for account %q; ignoring", err.Error(), *p.account.ID)
		} else {
			return errors.NewInternalServer(fmt.Sprintf("unexpected error creating policy %s", policy_name), err)
		}
	} else {
		// no error means we create the policy without issue moving on
		//p.account.PrincipalPolicyHash = policyHash
		return nil
	}

	// Prune old versions of the policy.  Making sure we have room for one more policy version
	// err = p.prunePolicyVersions()
	// if err != nil {
	// 	return err
	// }

	// Create a new Policy Version and set as default
	// _, err = p.iamSvc.CreatePolicyVersion(&iam.CreatePolicyVersionInput{
	// 	PolicyArn:      aws.String(p.account.PrincipalPolicyArn.String()),
	// 	PolicyDocument: policy,
	// 	SetAsDefault:   aws.Bool(true),
	// })

	// p.account.PrincipalPolicyHash = policyHash
	// if err != nil {
	// 	return errors.NewInternalServer(fmt.Sprintf("unexpected error creating policy version %q", p.account.PrincipalPolicyArn.String()), err)
	// }

	return nil
}

func (p *principalService) buildPolicyBluepi(policy_name string, role_name string) (*string, *string, error) {
	log.Printf("MergePolicyBluepi")
	log.Printf("Policy Name :  %s", policy_name)
	log.Printf("Role Name:  %s", role_name)

	type principalPolicyInput struct {
		PrincipalPolicyArn   string
		PrincipalRoleArn     string
		PrincipalIAMDenyTags []string
		AdminRoleArn         string
		Regions              []string
		BluePiRoleArn        string
	}
	policy_s3_key := fmt.Sprintf("%s.%s", policy_name, "tmpl")
	log.Printf("policy_s3_key :  %s", policy_s3_key)

	bluepi_role_arn := fmt.Sprintf("arn:aws:iam::%s:role/%s", p.config.AccountID, role_name)
	log.Printf("bluepi_role_arn :  %s", bluepi_role_arn)

	policy, policyHash, err := p.storager.GetTemplateObject(p.config.S3BucketName, policy_s3_key,
		principalPolicyInput{
			PrincipalPolicyArn:   p.account.PrincipalPolicyArn.String(),
			PrincipalRoleArn:     p.account.PrincipalRoleArn.String(),
			PrincipalIAMDenyTags: p.config.PrincipalIAMDenyTags,
			AdminRoleArn:         p.account.AdminRoleArn.String(),
			Regions:              p.config.AllowedRegions,
			BluePiRoleArn:        bluepi_role_arn,
		})
	if err != nil {
		return nil, nil, err
	}

	return &policy, &policyHash, nil
}

func (p *principalService) AttachRoleWithPolicyBluepi(role_name string, policy_name string) error {

	log.Printf("AttachRoleWithPolicyBluepi")
	bluepi_policy_arn := fmt.Sprintf("arn:aws:iam::%s:policy/%s", p.config.AccountID, policy_name)
	log.Printf("bluepi_policy_arn : %s", bluepi_policy_arn)

	// Attach the policy to the role
	_, err := p.iamSvc.AttachRolePolicy(&iam.AttachRolePolicyInput{
		PolicyArn: aws.String(bluepi_policy_arn),
		RoleName:  aws.String(role_name),
	})
	if err != nil {
		if isAWSAlreadyExistsError(err) {
			log.Printf("%s: for account %q; ignoring", err.Error(), *p.account.ID)
		} else {
			return errors.NewInternalServer(
				fmt.Sprintf("unexpected error attaching policy %q to role %q", bluepi_policy_arn, role_name),
				err)
		}
	}

	return nil
}
