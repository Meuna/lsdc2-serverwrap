package internal

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/zap/zapcore"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	cwTypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/smithy-go"
)

type CloudWatchCore struct {
	level         zapcore.Level
	client        *cloudwatchlogs.Client
	logGroupName  string
	logStreamName string
	mu            sync.Mutex
	sequenceToken *string
	batch         []cwTypes.InputLogEvent
	batchSize     int
	maxBatchSize  int
	flushInterval time.Duration
	flushTicker   *time.Ticker
	encoder       zapcore.Encoder
}

func NewCloudWatchCore(level zapcore.Level, logGroupName, logStreamName string, maxBatchSize int, flushInterval time.Duration) (*CloudWatchCore, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, err
	}

	client := cloudwatchlogs.NewFromConfig(cfg)

	// Ensure the log group and stream exist
	_, err = client.CreateLogStream(context.TODO(), &cloudwatchlogs.CreateLogStreamInput{
		LogGroupName:  aws.String(logGroupName),
		LogStreamName: aws.String(logStreamName),
	})
	if err != nil && !isResourceAlreadyExistsError(err) {
		return nil, err
	}

	// Init a standard zap JSON encoder
	encoder := zapcore.NewJSONEncoder(zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.EpochTimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	})

	core := &CloudWatchCore{
		level:         level,
		client:        client,
		logGroupName:  logGroupName,
		logStreamName: logStreamName,
		batch:         []cwTypes.InputLogEvent{},
		maxBatchSize:  maxBatchSize,
		flushInterval: flushInterval,
		flushTicker:   time.NewTicker(flushInterval),
		encoder:       encoder,
	}

	// Start a goroutine to flush logs periodically
	go core.flushPeriodically()

	return core, nil
}

func (c *CloudWatchCore) Enabled(level zapcore.Level) bool {
	return level >= c.level
}

func (c *CloudWatchCore) With(fields []zapcore.Field) zapcore.Core {
	return c
}

func (c *CloudWatchCore) Check(entry zapcore.Entry, checkedEntry *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return checkedEntry.AddCore(entry, c)
	}
	return checkedEntry
}

func (c *CloudWatchCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Format the log message
	buffer, err := c.encoder.EncodeEntry(entry, fields)
	if err != nil {
		return err
	}

	// Add the log event to the batch
	c.batch = append(c.batch, types.InputLogEvent{
		Message:   aws.String(buffer.String()),
		Timestamp: aws.Int64(entry.Time.UnixMilli()),
	})

	// Flush if the batch size exceeds the maximum
	if len(c.batch) >= c.maxBatchSize {
		return c.flush()
	}

	return nil
}

func (c *CloudWatchCore) Sync() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.flush()
}

func (c *CloudWatchCore) flush() error {
	if len(c.batch) == 0 {
		return nil
	}

	// Send the batch to CloudWatch Logs
	input := &cloudwatchlogs.PutLogEventsInput{
		LogGroupName:  aws.String(c.logGroupName),
		LogStreamName: aws.String(c.logStreamName),
		LogEvents:     c.batch,
		SequenceToken: c.sequenceToken,
	}

	output, err := c.client.PutLogEvents(context.TODO(), input)
	if err != nil {
		return err
	}

	// Update the sequence token
	c.sequenceToken = output.NextSequenceToken
	c.batch = []cwTypes.InputLogEvent{} // Clear the batch
	return nil
}

func (c *CloudWatchCore) flushPeriodically() {
	for range c.flushTicker.C {
		c.Sync()
	}
}

func isResourceAlreadyExistsError(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "ResourceAlreadyExistsException"
}
