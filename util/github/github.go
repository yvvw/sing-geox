package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	googleGithub "github.com/google/go-github/v45/github"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/exceptions"

	"sing-geox/util/env"
	myHttp "sing-geox/util/http"
)

var client *googleGithub.Client

func getClient() *googleGithub.Client {
	if client == nil {
		token, ok := env.GetAccessToken()
		var httpClient *http.Client
		if ok {
			httpClient = (&googleGithub.BasicAuthTransport{
				Username: token,
			}).Client()
		}
		client = googleGithub.NewClient(httpClient)
	}
	return client
}

func GetLatestRelease(ctx context.Context, repository string) (*googleGithub.RepositoryRelease, error) {
	info := strings.SplitN(repository, "/", 2)
	release, _, err := getClient().Repositories.GetLatestRelease(ctx, info[0], info[1])
	if err != nil {
		return nil, err
	}
	return release, err
}

func SafeGetReleaseFileBytes(release *googleGithub.RepositoryRelease, fileName string) ([]byte, error) {
	asset := common.Find(release.Assets, func(it *googleGithub.ReleaseAsset) bool {
		return *it.Name == fileName
	})
	if asset == nil {
		return nil, exceptions.New(fileName + " not found in " + *release.Name)
	}

	checksumFileName := fileName + ".sha256sum"
	checksumAsset := common.Find(release.Assets, func(it *googleGithub.ReleaseAsset) bool {
		return *it.Name == checksumFileName
	})
	if checksumAsset == nil {
		return nil, exceptions.New(checksumFileName + " not found in " + *release.Name)
	}

	fileBytes, err := myHttp.GetBytes(asset.BrowserDownloadURL)
	if err != nil {
		return nil, err
	}

	checksumBytes, err := myHttp.GetBytes(checksumAsset.BrowserDownloadURL)
	if err != nil {
		return nil, err
	}

	checksum := sha256.Sum256(fileBytes)
	if hex.EncodeToString(checksum[:]) != string(checksumBytes[:64]) {
		return nil, exceptions.New(fileName + " checksum mismatch")
	}

	return fileBytes, nil
}
