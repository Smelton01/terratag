package tagging

import (
	"log"
	"regexp"
	"strings"

	"github.com/env0/terratag/internal/common"
	"github.com/env0/terratag/internal/convert"
	"github.com/env0/terratag/internal/tag_keys"
	"github.com/env0/terratag/internal/terraform"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
)

func defaultTaggingFn(args TagBlockArgs) (*Result, error) {
	tagBlock, err := TagBlock(args)
	if err != nil {
		return nil, err
	}

	return &Result{SwappedTagsStrings: []string{tagBlock}}, nil
}

func ParseHclValueStringToTokens(hclValueString string) hclwrite.Tokens {
	file, diags := hclwrite.ParseConfig([]byte("tempKey = "+strings.TrimSpace(hclValueString)), "", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		log.Print("error parsing hcl value string " + hclValueString)
		panic(diags.Errs()[0])
	}
	tempAttribute := file.Body().GetAttribute("tempKey")
	return tempAttribute.Expr().BuildTokens(hclwrite.Tokens{})
}

func TagBlock(args TagBlockArgs) (string, error) {
	hasExistingTags, err := convert.MoveExistingTags(args.Filename, args.Terratag, args.Block, args.TagId)
	if err != nil {
		return "", err
	}

	terratagAddedKey := "local." + tag_keys.GetTerratagAddedKey(args.Filename)
	newTagsValue := terratagAddedKey

	if hasExistingTags {
		existingTagsKey := tag_keys.GetResourceExistingTagsKey(args.Filename, args.Block)
		existingTagsExpression := convert.GetExistingTagsExpression(args.Terratag.Found[existingTagsKey], args.TfVersion)
		newTagsValue = "merge( " + existingTagsExpression + ", " + terratagAddedKey + ")"
	}

	if args.TfVersion.Major == 0 && args.TfVersion.Minor == 11 {
		newTagsValue = "\"${" + newTagsValue + "}\""
	}

	newTagsValueTokens := ParseHclValueStringToTokens(newTagsValue)
	args.Block.Body().SetAttributeRaw(args.TagId, newTagsValueTokens)

	return newTagsValue, nil
}

// RemoveTerratagTags removes the terratag added tags from a resource
func RemoveTerratagTags(args TagBlockArgs) (*Result, error) {
	log.Println("[INFO] Removing terratag tags from resource: ", args.Block.Type(), args.Filename)
	hasExistingTags, err := convert.MoveExistingTags(args.Filename, args.Terratag, args.Block, args.TagId)
	if err != nil {
		return &Result{SwappedTagsStrings: []string{}}, err
	}

	if !hasExistingTags {
		log.Println("[INFO] No existing tags found, nothing to remove...", args.Filename)
		return &Result{SwappedTagsStrings: []string{}}, nil
	}

	existingTagsKey := tag_keys.GetResourceExistingTagsKey(args.Filename, args.Block)
	existingTagsExpression := convert.GetExistingTagsExpression(args.Terratag.Found[existingTagsKey], args.TfVersion)

	// regex match an object/map in the existing tag expression
	r, err := regexp.Compile(`(?s)\{.*\}`)
	if err != nil {
		log.Fatal(err)
		return &Result{SwappedTagsStrings: []string{}}, err
	}

	hasOrigTags := r.MatchString(existingTagsExpression)
	if !hasOrigTags {
		log.Println("[INFO] No original tags found, removing tag block from resource: ", args.Filename)
		args.Block.Body().RemoveAttribute(args.TagId)

		return &Result{SwappedTagsStrings: []string{existingTagsKey}}, nil
	}

	origTagsValue := r.FindString(existingTagsExpression)
	origTagsValueTokens := ParseHclValueStringToTokens(origTagsValue)

	args.Block.Body().SetAttributeRaw(args.TagId, origTagsValueTokens)

	return &Result{SwappedTagsStrings: []string{origTagsValue}}, nil
}

func HasResourceTagFn(resourceType string) bool {
	return resourceTypeToFnMap[resourceType] != nil
}

func TagResource(args TagBlockArgs) (*Result, error) {
	resourceType := terraform.GetResourceType(*args.Block)

	customTaggingFn := resourceTypeToFnMap[resourceType]

	if customTaggingFn != nil {
		return customTaggingFn(args)
	} else {
		return defaultTaggingFn(args)
	}
}

var resourceTypeToFnMap = map[string]TagResourceFn{
	"aws_autoscaling_group":      tagAutoscalingGroup,
	"google_container_cluster":   tagContainerCluster,
	"azurerm_kubernetes_cluster": tagAksK8sCluster,
}

type TagBlockArgs struct {
	Filename  string
	Block     *hclwrite.Block
	Tags      string
	Terratag  common.TerratagLocal
	TagId     string
	TfVersion common.Version
}

type TagResourceFn func(args TagBlockArgs) (*Result, error)

type Result struct {
	SwappedTagsStrings []string
}
