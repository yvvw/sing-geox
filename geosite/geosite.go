package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/go-github/v62/github"
	"github.com/sagernet/sing-box/common/geosite"
	"github.com/sagernet/sing-box/common/srs"
	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	"github.com/sirupsen/logrus"
	"github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"google.golang.org/protobuf/proto"

	myGithub "github.com/yvvw/sing-geox/util/github"
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

	err = generate(fullRelease, fullInput, fullOutput, "rule-set")
	if err != nil {
		logrus.Fatal(err)
	}
	err = generate(liteRelease, liteInput, liteOutput, "")
	if err != nil {
		logrus.Fatal(err)
	}
}

func generate(release *github.RepositoryRelease, inputFileName string, outputFileName string, ruleSetDir string) (err error) {
	data, err := myGithub.GetReleaseFile(release, inputFileName)
	if err != nil {
		return err
	}

	siteMap, err := parseGeoSite(data)
	filterBadCode(siteMap)
	mergeCNSites(siteMap)
	if err != nil {
		return err
	}

	err = writeSite(siteMap, outputFileName+".db")
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

	for _, item := range list.Entry {
		code := strings.ToLower(item.CountryCode)
		domains := make([]geosite.Item, 0, len(item.Domain)*2)
		attributeMap := make(map[string][]*routercommon.Domain)
		for _, domain := range item.Domain {
			if len(domain.Attribute) > 0 {
				for _, attribute := range domain.Attribute {
					attributeMap[attribute.Key] = append(attributeMap[attribute.Key], domain)
				}
			}
			sites := domain2site(domain)
			for _, site := range sites {
				domains = append(domains, site)
			}
		}

		siteMap[code] = common.Uniq(domains)
		for attribute, attributeEntries := range attributeMap {
			attributeDomains := make([]geosite.Item, 0, len(attributeEntries)*2)
			for _, domain := range attributeEntries {
				sites := domain2site(domain)
				for _, site := range sites {
					attributeDomains = append(attributeDomains, site)
				}
			}
			siteMap[code+"@"+attribute] = common.Uniq(attributeDomains)
		}
	}
	return siteMap, nil
}

func domain2site(domain *routercommon.Domain) (items []geosite.Item) {
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

type badCode struct {
	code    string
	badCode string
}

func filterBadCode(siteMap map[string][]geosite.Item) {
	var codeList []string
	for code := range siteMap {
		codeList = append(codeList, code)
	}
	var badCodeList []badCode

	var filteredCodeList []string
	for _, code := range codeList {
		codeParts := strings.Split(code, "@")
		if len(codeParts) != 2 {
			continue
		}
		leftParts := strings.Split(codeParts[0], "-")
		var lastName string
		if len(leftParts) > 1 {
			lastName = leftParts[len(leftParts)-1]
		}
		if lastName == "" {
			lastName = codeParts[0]
		}
		if lastName == codeParts[1] {
			delete(siteMap, code)
			filteredCodeList = append(filteredCodeList, code)
			continue
		}
		if "!"+lastName == codeParts[1] {
			badCodeList = append(badCodeList, badCode{
				code:    codeParts[0],
				badCode: code,
			})
		} else if lastName == "!"+codeParts[1] {
			badCodeList = append(badCodeList, badCode{
				code:    codeParts[0],
				badCode: code,
			})
		}
	}

	var mergedCodeList []string
	for _, it := range badCodeList {
		badList := siteMap[it.badCode]
		if badList == nil {
			panic("bad list not found: " + it.badCode)
		}
		delete(siteMap, it.badCode)
		newMap := make(map[geosite.Item]bool)
		for _, item := range siteMap[it.code] {
			newMap[item] = true
		}
		for _, item := range badList {
			delete(newMap, item)
		}
		newList := make([]geosite.Item, 0, len(newMap))
		for item := range newMap {
			newList = append(newList, item)
		}
		siteMap[it.code] = newList
		mergedCodeList = append(mergedCodeList, it.badCode)
	}
}

func mergeCNSites(siteMap map[string][]geosite.Item) {
	var codeList []string
	for code := range siteMap {
		codeList = append(codeList, code)
	}

	var cnCodeList []string
	for _, code := range codeList {
		codeParts := strings.Split(code, "@")
		if len(codeParts) != 2 {
			continue
		}
		if codeParts[1] != "cn" {
			continue
		}
		if !strings.HasPrefix(codeParts[0], "category-") {
			continue
		}
		if strings.HasSuffix(codeParts[0], "-cn") || strings.HasSuffix(codeParts[0], "-!cn") {
			continue
		}
		cnCodeList = append(cnCodeList, code)
	}

	newCNMap := make(map[geosite.Item]bool)
	for _, site := range siteMap["geolocation-cn"] {
		newCNMap[site] = true
	}
	for _, code := range cnCodeList {
		for _, site := range siteMap[code] {
			newCNMap[site] = true
		}
	}
	newCNSites := make([]geosite.Item, 0, len(newCNMap))
	for site := range newCNMap {
		newCNSites = append(newCNSites, site)
	}
	siteMap["geolocation-cn"] = newCNSites
}

func writeSite(siteMap map[string][]geosite.Item, fileName string) (err error) {
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
	for name := range siteMap {
		siteNames = append(siteNames, name)
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

	for code, sites := range siteMap {
		filePath, _ := filepath.Abs(filepath.Join(ruleSetPath, "geosite-"+code))
		err := writeRuleSetItem(sites, filePath)
		if err != nil {
			return err
		}
	}
	return err
}

func writeRuleSetItem(sites []geosite.Item, filePath string) (err error) {
	defaultRule := geosite.Compile(sites)

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
	for _, domain := range sites {
		siteTxt += domain.Value + "\n"
	}

	_, err = txtFile.WriteString(siteTxt)
	if err != nil {

		return err
	}

	return err
}
