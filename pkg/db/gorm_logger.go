package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	gormlogger "gorm.io/gorm/logger"
)

type zapGormLogger struct {
	zap                  *zap.Logger
	logLevel             gormlogger.LogLevel
	slowThreshold        time.Duration
	ignoreRecordNotFound bool
}

func NewZapGormLogger(z *zap.Logger) gormlogger.Interface {
	return &zapGormLogger{
		zap:                  z.WithOptions(zap.AddCallerSkip(3)),
		logLevel:             gormlogger.Info,
		slowThreshold:        200 * time.Millisecond,
		ignoreRecordNotFound: true,
	}
}

func (l *zapGormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	newLogger := *l
	newLogger.logLevel = level
	return &newLogger
}

func (l *zapGormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	if l.logLevel >= gormlogger.Info {
		l.zap.Info(fmt.Sprintf(msg, data...))
	}
}

func (l *zapGormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if l.logLevel >= gormlogger.Warn {
		l.zap.Warn(fmt.Sprintf(msg, data...))
	}
}

func (l *zapGormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	if l.logLevel >= gormlogger.Error {
		l.zap.Error(fmt.Sprintf(msg, data...))
	}
}

func (l *zapGormLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if l.logLevel <= gormlogger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()

	fields := []zap.Field{
		zap.String("sql", sql),
		zap.Int64("rows", rows),
		zap.Duration("elapsed", elapsed),
	}

	switch {
	case err != nil && !(l.ignoreRecordNotFound && errors.Is(err, gormlogger.ErrRecordNotFound)):
		l.zap.Error("gorm query error", append(fields, zap.Error(err))...)

	case elapsed > l.slowThreshold && l.slowThreshold > 0 && l.logLevel >= gormlogger.Warn:
		l.zap.Warn("gorm slow query", fields...)

	case l.logLevel >= gormlogger.Info:
		l.zap.Info("gorm query", fields...)
	}
}
