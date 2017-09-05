package meta

import (
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sts"
)

type Metadata struct {
	AccountID, Region string
}

//TODO: write test and maybe return error also?
func userIDFromARN(arn string) string {
	splitARN := strings.Split(arn, ":")
	return splitARN[4]
}

func Data(sess *session.Session) *Metadata {
	// used to get account ID (needed to know the
	// bucket's object prefix)
	stsClient := sts.New(sess)
	req, userResp := stsClient.GetCallerIdentityRequest(&sts.GetCallerIdentityInput{})
	if err := req.Send(); err != nil {
		fmt.Fprintln(os.Stderr, "Error trying to get account ID: ", err)
		os.Exit(1)
	}

	return &Metadata{
		AccountID: userIDFromARN(*userResp.Arn),
		Region:    *sess.Config.Region,
	}
}
