package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/go-github/v45/github"
	"github.com/sagernet/sing-box/common/geosite"
	"github.com/sagernet/sing-box/common/srs"
	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	"github.com/sirupsen/logrus"
	"github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"google.golang.org/protobuf/proto"

	myGithub "sing-geox/util/github"
)

func main() {
	fullRepo := "Loyalsoldier/v2ray-rules-dat"
	fullInput := "geosite.dat"
	fullOutput := "geosite-full"

	liteRepo := "v2fly/domain-list-community"
	liteInput := "dlc.dat"
	liteOutput := "geosite-lite"

	ctx := context.Background()
	fullRelease, err := myGithub.GetLatestRelease(ctx, fullRepo)
	if err != nil {
		logrus.Fatal(err)
	}
	liteRelease, err := myGithub.GetLatestRelease(ctx, liteRepo)
	if err != nil {
		logrus.Fatal(err)
	}

	err = generateGeoSite(fullRelease, fullInput, fullOutput, "rule-set")
	if err != nil {
		logrus.Fatal(err)
	}
	err = generateGeoSite(liteRelease, liteInput, liteOutput, "")
	if err != nil {
		logrus.Fatal(err)
	}
}

func generateGeoSite(release *github.RepositoryRelease, inputFileName string, outputFileName string, ruleSetDir string) (err error) {
	data, err := myGithub.SafeGetReleaseFileBytes(release, inputFileName)
	if err != nil {
		return err
	}

	siteMap, err := parseGeoSite(data)
	if err != nil {
		return err
	}

	err = writeGeoSite(siteMap, outputFileName+".db")
	if err != nil {
		return err
	}

	err = writeSiteText(siteMap, outputFileName+".txt")
	if err != nil {
		return err
	}

	if ruleSetDir != "" {
		err = writeRuleSet(siteMap, ruleSetDir)
		if err != nil {
			return err
		}
	}
	return err
}

func parseGeoSite(data []byte) (siteMap map[string][]geosite.Item, err error) {
	list := routercommon.GeoSiteList{}
	err = proto.Unmarshal(data, &list)
	if err != nil {
		return nil, err
	}

	siteMap = make(map[string][]geosite.Item)

	for _, entry := range list.Entry {
		code := strings.ToLower(entry.CountryCode)
		domains := make([]geosite.Item, 0, len(entry.Domain)*2)
		attributeMap := make(map[string][]*routercommon.Domain)
		for _, domain := range entry.Domain {
			if len(domain.Attribute) > 0 {
				for _, attribute := range domain.Attribute {
					attributeMap[attribute.Key] = append(attributeMap[attribute.Key], domain)
				}
			}
			items := domain2item(domain)
			for _, item := range items {
				domains = append(domains, item)
			}
		}

		siteMap[code] = common.Uniq(domains)

		for attribute, attributeEntries := range attributeMap {
			attributeDomains := make([]geosite.Item, 0, len(attributeEntries)*2)
			for _, domain := range attributeEntries {
				items := domain2item(domain)
				for _, item := range items {
					attributeDomains = append(attributeDomains, item)
				}
			}
			siteMap[code+"@"+attribute] = common.Uniq(attributeDomains)
		}
	}
	return siteMap, nil
}

func domain2item(domain *routercommon.Domain) (items []geosite.Item) {
	switch domain.Type {
	case routercommon.Domain_Plain:
		items = append(items, geosite.Item{
			Type:  geosite.RuleTypeDomainKeyword,
			Value: domain.Value,
		})
	case routercommon.Domain_Regex:
		items = append(items, geosite.Item{
			Type:  geosite.RuleTypeDomainRegex,
			Value: domain.Value,
		})
	case routercommon.Domain_RootDomain:
		if strings.Contains(domain.Value, ".") {
			items = append(items, geosite.Item{
				Type:  geosite.RuleTypeDomain,
				Value: domain.Value,
			})
		}
		items = append(items, geosite.Item{
			Type:  geosite.RuleTypeDomainSuffix,
			Value: "." + domain.Value,
		})
	case routercommon.Domain_Full:
		items = append(items, geosite.Item{
			Type:  geosite.RuleTypeDomain,
			Value: domain.Value,
		})
	}
	return items
}

func writeGeoSite(siteMap map[string][]geosite.Item, fileName string) (err error) {
	file, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	err = geosite.Write(file, siteMap)
	if err != nil {
		return err
	}
	return err
}

func writeSiteText(siteMap map[string][]geosite.Item, fileName string) error {
	file, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	var siteNames []string
	for siteName := range siteMap {
		siteNames = append(siteNames, siteName)
	}
	sort.Strings(siteNames)

	_, err = file.WriteString(strings.Join(siteNames, "\n"))
	return err
}

func writeRuleSet(siteMap map[string][]geosite.Item, ruleSetPath string) (err error) {
	err = os.MkdirAll(ruleSetPath, 0o755)
	if err != nil {
		return err
	}

	for code, domains := range siteMap {
		filePath, _ := filepath.Abs(filepath.Join(ruleSetPath, "geosite-"+code))
		err := writeRuleSetItem(domains, filePath)
		if err != nil {
			return err
		}
	}
	return err
}

func writeRuleSetItem(domains []geosite.Item, filePath string) (err error) {
	defaultRule := geosite.Compile(domains)

	var rule option.DefaultHeadlessRule
	rule.Domain = defaultRule.Domain
	rule.DomainSuffix = defaultRule.DomainSuffix
	rule.DomainKeyword = defaultRule.DomainKeyword
	rule.DomainRegex = defaultRule.DomainRegex

	var plain option.PlainRuleSet
	plain.Rules = []option.HeadlessRule{
		{
			Type:           constant.RuleTypeDefault,
			DefaultOptions: rule,
		},
	}

	srsFile, err := os.Create(filePath + ".srs")
	if err != nil {
		return err
	}
	defer srsFile.Close()

	err = srs.Write(srsFile, plain)
	if err != nil {
		return err
	}

	txtFile, err := os.Create(filePath + ".txt")
	if err != nil {
		return err
	}
	defer txtFile.Close()

	siteTxt := ""
	for _, domain := range domains {
		siteTxt += domain.Value + "\n"
	}

	_, err = txtFile.WriteString(siteTxt)
	if err != nil {

		return err
	}

	return err
}
