package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/go-github/v58/github"
	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/inserter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
	"github.com/oschwald/geoip2-golang"
	"github.com/oschwald/maxminddb-golang"
	"github.com/sagernet/sing-box/common/srs"
	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sirupsen/logrus"

	myGithub "sing-geox/util/github"
)

func main() {
	repo := "soffchen/geoip"
	input := "Country.mmdb"
	output := "geoip"
	outputCN := "geoip-cn"

	release, err := myGithub.GetLatestRelease(context.Background(), repo)
	if err != nil {
		logrus.Fatal(err)
	}

	err = generate(release, input, output, outputCN, "rule-set")
	if err != nil {
		logrus.Fatal(err)
	}
}

func generate(release *github.RepositoryRelease, inputFileName string, outputFileName string, outputCNFileName string, ruleSetDir string) error {
	data, err := myGithub.GetReleaseFile(release, inputFileName)
	if err != nil {
		return err
	}

	metadata, ipMap, err := parseGeoIp(data)
	if err != nil {
		return err
	}

	err = writeIp(metadata, ipMap, outputFileName+".db", outputCNFileName+".db")
	if err != nil {
		return err
	}

	err = writeIpText(ipMap, outputFileName+".txt")
	if err != nil {
		return err
	}

	if ruleSetDir != "" {
		err = writeRuleSet(ipMap, ruleSetDir)
		if err != nil {
			return err
		}
	}
	return nil
}

func parseGeoIp(data []byte) (metadata maxminddb.Metadata, ipMap map[string][]*net.IPNet, err error) {
	db, err := maxminddb.FromBytes(data)
	if err != nil {
		return
	}

	metadata = db.Metadata
	ipMap = make(map[string][]*net.IPNet)

	networks := db.Networks(maxminddb.SkipAliasedNetworks)
	var enterprise geoip2.Enterprise
	var ipNet *net.IPNet
	for networks.Next() {
		ipNet, err = networks.Network(&enterprise)
		if err != nil {
			return
		}
		var code string
		if enterprise.Country.IsoCode != "" {
			code = strings.ToLower(enterprise.Country.IsoCode)
		} else if enterprise.RegisteredCountry.IsoCode != "" {
			code = strings.ToLower(enterprise.RegisteredCountry.IsoCode)
		} else if enterprise.RepresentedCountry.IsoCode != "" {
			code = strings.ToLower(enterprise.RepresentedCountry.IsoCode)
		} else if enterprise.Continent.Code != "" {
			code = strings.ToLower(enterprise.Continent.Code)
		} else {
			continue
		}
		ipMap[code] = append(ipMap[code], ipNet)
	}
	err = networks.Err()
	return
}

func writeIp(metadata maxminddb.Metadata, ipMap map[string][]*net.IPNet, fileName string, cnFileName string) (err error) {
	err = writeIpData(metadata, ipMap, nil, fileName)
	if err != nil {
		return err
	}

	err = writeIpData(metadata, ipMap, []string{"cn"}, cnFileName)
	if err != nil {
		return err
	}

	return err
}

func writeIpData(metadata maxminddb.Metadata, ipMap map[string][]*net.IPNet, codes []string, fileName string) (err error) {
	if len(codes) == 0 {
		codes = make([]string, 0, len(ipMap))
		for code := range ipMap {
			codes = append(codes, code)
		}
	}
	sort.Strings(codes)

	writer, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            "sing-geoip",
		Languages:               codes,
		IPVersion:               int(metadata.IPVersion),
		RecordSize:              int(metadata.RecordSize),
		Inserter:                inserter.ReplaceWith,
		DisableIPv4Aliasing:     true,
		IncludeReservedNetworks: true,
	})
	if err != nil {
		return err
	}

	codeMap := make(map[string]bool)
	for _, code := range codes {
		codeMap[code] = true
	}

	for code, ipNets := range ipMap {
		if !codeMap[code] {
			continue
		}
		for _, ip := range ipNets {
			err := writer.Insert(ip, mmdbtype.String(code))
			if err != nil {
				return err
			}
		}
	}

	file, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = writer.WriteTo(file)
	return err
}

func writeIpText(ipMap map[string][]*net.IPNet, fileName string) error {
	file, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	var ipNames []string
	for name := range ipMap {
		ipNames = append(ipNames, name)
	}
	sort.Strings(ipNames)

	_, err = file.WriteString(strings.Join(ipNames, "\n"))
	return err
}

func writeRuleSet(ipMap map[string][]*net.IPNet, ruleSetPath string) (err error) {
	err = os.MkdirAll(ruleSetPath, 0o755)
	if err != nil {
		return err
	}

	for code, ips := range ipMap {
		filePath, _ := filepath.Abs(filepath.Join(ruleSetPath, "geoip-"+code))
		err := writeRuleSetItem(ips, filePath)
		if err != nil {
			return err
		}
	}
	return err
}

func writeRuleSetItem(ips []*net.IPNet, filePath string) (err error) {
	var rule option.DefaultHeadlessRule
	rule.IPCIDR = make([]string, 0, len(ips))
	for _, cidr := range ips {
		rule.IPCIDR = append(rule.IPCIDR, cidr.String())
	}

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
	return err
}
