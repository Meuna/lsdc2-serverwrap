package internal

import (
	"context"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

func downloadFromS3(bucket string, key string, fpath string, uid int, gid int) error {
	w, err := os.Create(fpath)
	if err != nil {
		return err
	}
	defer os.Chown(fpath, uid, gid)
	return streamDownloadFromS3(bucket, key, w)
}

func uploadToS3(bucket string, key string, fpath string) error {
	r, err := os.Open(fpath)
	if err != nil {
		return err
	}
	return streamUploadToS3(bucket, key, r)
}

func streamDownloadFromS3(bucket string, key string, w io.WriterAt) error {
	client, err := getS3Client()
	if err != nil {
		return err
	}

	downloader := manager.NewDownloader(client)
	_, err = downloader.Download(context.TODO(), w, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return err
}

func s3Get(bucket string, key string) (*s3.GetObjectOutput, error) {
	client, err := getS3Client()
	if err != nil {
		return nil, err
	}

	out, err := client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return out, err
}

func streamUploadToS3(bucket string, key string, r io.Reader) error {
	client, err := getS3Client()
	if err != nil {
		return err
	}

	uploader := manager.NewUploader(client)
	_, err = uploader.Upload(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   r,
	})
	return err
}

func queueMessage(queueUrl string, msg string) error {
	client, err := getSqsClient()
	if err != nil {
		return err
	}

	_, err = client.SendMessage(context.TODO(), &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueUrl),
		MessageBody: aws.String(msg),
	})

	return err
}

func getS3Client() (*s3.Client, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, err
	}

	return s3.NewFromConfig(cfg), nil
}

func getSqsClient() (*sqs.Client, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, err
	}

	return sqs.NewFromConfig(cfg), nil
}
