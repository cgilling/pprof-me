package store

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/cgilling/pprof-me/msg"
	"github.com/google/uuid"
)

var (
	ErrIDFormat = errors.New("id format not as expected")
)

// NOTE: for now the default AWS envvars are assumed to be set:
//		 - AWS_ACCESS_KEY_ID or AWS_ACCESS_KEY
//		 - AWS_SECRET_ACCESS_KEY or AWS_SECRET_KEY
//       - AWS_REGION

type AWSConfig struct {
	BucketName string `yaml:"s3_bucket" envconfig:"S3_BUCKET"`

	// TODO: provide a way to have a non default HTTP client

	// intended for local testing
	S3Endpoint string `yaml:"s3_endpoint" envconfig:"S3_ENDPOINT"`
}

type AWSStore struct {
	config   AWSConfig
	sesssion *session.Session
	s3cli    s3iface.S3API
}

func NewAWSStore(config AWSConfig) (*AWSStore, error) {
	sess, err := session.NewSession()
	if err != nil {
		return nil, err
	}
	extraConf := &aws.Config{}
	if config.S3Endpoint != "" {
		extraConf.Endpoint = &config.S3Endpoint
		extraConf.S3ForcePathStyle = aws.Bool(true)
	}
	return &AWSStore{
		config:   config,
		sesssion: sess,
		s3cli:    s3.New(sess, extraConf),
	}, nil
}

func (as *AWSStore) CreateID(ctx context.Context, appName string) string {
	// TODO: we can switch this func to return an error and if a valid app name
	//		 is not supplied, then return an error
	uid, _ := uuid.NewUUID()
	id := uid.String()
	appName = base64.URLEncoding.EncodeToString([]byte(appName))
	seconds, _ := uid.Time().UnixTime()
	// NOTE: we invert the digits here so that the sort order in s3 will
	//		 be newest to oldest.
	invertedSeconds := invertedDigitConv(seconds)
	// TODO: consider having the ID just be the second two parts, since we are using
	//		 uuid.v1, this already contains that timestamp within, then we can just
	//		 use the three parts as the path in the s3 bucket, but not the id itself.
	return invertedSeconds + ":" + appName + ":" + id
}

func invertedDigitConv(i int64) string {
	b := strconv.AppendInt([]byte{}, i, 10)
	for i, c := range b {
		var newc byte
		switch c {
		case '0':
			newc = '9'
		case '1':
			newc = '8'
		case '2':
			newc = '7'
		case '3':
			newc = '6'
		case '4':
			newc = '5'
		case '5':
			newc = '4'
		case '6':
			newc = '3'
		case '7':
			newc = '2'
		case '8':
			newc = '1'
		case '9':
			newc = '0'
		default:
			panic(fmt.Errorf("unexpected input byte: %X", c))
		}
		b[i] = newc
	}
	return string(b)
}

func parseID(id string) (appName string, uid uuid.UUID, err error) {
	parts := strings.Split(id, ":")
	if len(parts) != 3 {
		return "", uuid.UUID{}, ErrIDFormat
	}
	appBytes, err := base64.URLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", uuid.UUID{}, err
	}
	uid, err = uuid.Parse(parts[2])
	if err != nil {
		return "", uuid.UUID{}, err
	}
	return string(appBytes), uid, nil
}

func (as *AWSStore) ListProfiles(ctx context.Context) ([]msg.ProfileListInfo, error) {
	var resp []msg.ProfileListInfo
	input := &s3.ListObjectsInput{
		Bucket:  aws.String(as.config.BucketName),
		MaxKeys: aws.Int64(500),
	}

	result, err := as.s3cli.ListObjects(input)
	if err != nil {
		return nil, err
	}
	for _, obj := range result.Contents {
		appName, uid, err := parseID(*obj.Key)
		if err != nil {
			log.Println(err)
			continue
		}
		resp = append(resp, msg.ProfileListInfo{
			ID:        *obj.Key,
			AppName:   appName,
			Timestamp: time.Unix(uid.Time().UnixTime()),
		})
	}
	return resp, nil
}

func (as *AWSStore) StoreProfile(ctx context.Context, id string, profile []byte, meta ProfileMetadata) error {
	_, _, err := parseID(id)
	if err != nil {
		return err
	}

	_, err = as.s3cli.PutObjectWithContext(ctx, &s3.PutObjectInput{
		Bucket: aws.String(as.config.BucketName),
		Key:    aws.String(id),
		Body:   bytes.NewReader(profile),
	})
	if err != nil {
		return err
	}
	return nil
}

func (as *AWSStore) GetProfile(ctx context.Context, id string) ([]byte, ProfileMetadata, error) {
	appName, uid, err := parseID(id)
	if err != nil {
		return nil, ProfileMetadata{}, err
	}

	input := &s3.GetObjectInput{
		Bucket: aws.String(as.config.BucketName),
		Key:    aws.String(id),
	}

	result, err := as.s3cli.GetObjectWithContext(ctx, input)
	if err != nil {
		return nil, ProfileMetadata{}, err
	}
	b, err := ioutil.ReadAll(result.Body)
	if err != nil {
		return nil, ProfileMetadata{}, err
	}
	meta := ProfileMetadata{
		AppName:   appName,
		Timestamp: time.Unix(uid.Time().UnixTime()),
	}
	return b, meta, nil
}
