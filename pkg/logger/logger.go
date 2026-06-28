package logger

import (
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var log *zap.Logger

// Init khởi tạo global logger với JSON encoder.
// serviceName: tên service (vd: "identity", "notification") — được gắn vào mọi log entry
//   và trở thành Loki label sau khi Promtail parse.
// env: môi trường chạy (vd: "dev", "staging", "prod") — dùng để filter trên Grafana.
func Init(serviceName string, env string) error {
	cfg := zapcore.EncoderConfig{
		TimeKey:      "time",
		LevelKey:     "level",
		MessageKey:   "msg",
		CallerKey:    "caller",
		EncodeLevel:  zapcore.LowercaseLevelEncoder,
		// RFC3339Nano (vd: "2025-10-30T16:23:43.123456789Z") — Promtail timestamp stage
		// nhận dạng được, giúp Grafana hiển thị đúng thời gian của log entry.
		EncodeTime:   func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(t.UTC().Format(time.RFC3339Nano))
		},
		EncodeCaller: zapcore.ShortCallerEncoder,
	}

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(cfg),
		zapcore.AddSync(os.Stdout),
		zap.InfoLevel,
	)

	fields := []zap.Field{
		zap.String("service", serviceName),
	}
	if env != "" {
		fields = append(fields, zap.String("env", env))
	}

	log = zap.New(core,
		zap.AddCaller(),
		zap.AddStacktrace(zap.ErrorLevel),
		zap.Fields(fields...),
	)

	return nil
}

func L() *zap.Logger {
	return log
}

