package trace

import (
	"context"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// InitTracer khởi tạo OpenTelemetry TracerProvider với OTLP HTTP exporter.
// Trả về provider để caller có thể gọi defer tp.Shutdown(ctx) khi tắt ứng dụng.
//
// Ví dụ sử dụng:
//
//	tp, err := trace.InitTracer(ctx, "notification-service", "http://jaeger:4318")
//	if err != nil { log.Fatal(err) }
//	defer tp.Shutdown(ctx)
func InitTracer(ctx context.Context, serviceName, collectorURL string) (*sdktrace.TracerProvider, error) {
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(collectorURL+"/v1/traces"),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			attribute.String("library.language", "go"),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		// Sampler: lấy mẫu 100% trong môi trường dev
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	// Đăng ký global TracerProvider và Propagator (W3C TraceContext chuẩn)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp, nil
}

// -------------------------------------------------------------------
// AMQPHeadersCarrier — Adapter cho RabbitMQ Header Propagation
// -------------------------------------------------------------------

// AMQPHeadersCarrier chuyển đổi map[string]interface{} (amqp.Table) thành
// TextMapCarrier tiêu chuẩn của OpenTelemetry để Inject/Extract context.
type AMQPHeadersCarrier map[string]interface{}

// Get trả về giá trị của header key từ AMQP table.
func (c AMQPHeadersCarrier) Get(key string) string {
	if val, ok := c[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// Set ghi giá trị header key vào AMQP table.
func (c AMQPHeadersCarrier) Set(key string, value string) {
	c[key] = value
}

// Keys trả về tất cả keys có trong AMQP table.
func (c AMQPHeadersCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// InjectAMQP injects trace context từ ctx vào AMQP headers map.
// Gọi hàm này trước khi publish message lên RabbitMQ.
func InjectAMQP(ctx context.Context, headers map[string]interface{}) {
	otel.GetTextMapPropagator().Inject(ctx, AMQPHeadersCarrier(headers))
}

// ExtractAMQP trích xuất trace context từ AMQP headers và trả về context mới.
// Gọi hàm này khi consume message từ RabbitMQ trước khi xử lý.
func ExtractAMQP(ctx context.Context, headers map[string]interface{}) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, AMQPHeadersCarrier(headers))
}

// -------------------------------------------------------------------
// Map-based Propagation — Dùng cho Outbox Pattern (qua Database)
// -------------------------------------------------------------------

// mapCarrier là adapter cho map[string]string để dùng với TextMapPropagator.
type mapCarrier map[string]string

func (m mapCarrier) Get(key string) string      { return m[key] }
func (m mapCarrier) Set(key string, val string)  { m[key] = val }
func (m mapCarrier) Keys() []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// InjectMap tuần tự hóa trace context hiện tại thành map[string]string.
// Dùng để lưu trace context vào cột JSON payload của bảng outbox_events trong DB.
//
// Ví dụ:
//
//	payload["trace_context"] = trace.InjectMap(ctx)
func InjectMap(ctx context.Context) map[string]string {
	carrier := make(mapCarrier)
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier
}

// ExtractMap phục hồi trace context từ map[string]string đã được lưu trước đó.
// Dùng trong Outbox Worker khi quét DB để tiếp tục chuỗi trace.
//
// Ví dụ:
//
//	ctx = trace.ExtractMap(ctx, payload["trace_context"])
func ExtractMap(ctx context.Context, m map[string]string) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, mapCarrier(m))
}

// -------------------------------------------------------------------
// TracerMiddleware — Gin HTTP Middleware
// -------------------------------------------------------------------

// TracerMiddleware là Gin middleware tự viết sử dụng OpenTelemetry tiêu chuẩn.
// Tự động:
//   - Extract trace context từ HTTP request headers (header traceparent).
//   - Tạo server-side span với đầy đủ thông tin HTTP (method, path, status).
//   - Ghi nhận lỗi và set span status khi response code >= 500.
//   - Inject span context vào Gin Context để các handler dùng trace.SpanFromContext().
func TracerMiddleware(serviceName string) gin.HandlerFunc {
	tracer := otel.Tracer(serviceName)

	return func(c *gin.Context) {
		// 1. Extract trace context từ HTTP headers đến (nếu có traceparent từ upstream)
		ctx := otel.GetTextMapPropagator().Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))

		// 2. Bắt đầu server span
		spanName := c.Request.Method + " " + c.FullPath()
		if spanName == " " {
			spanName = c.Request.Method + " " + c.Request.URL.Path
		}

		ctx, span := tracer.Start(ctx, spanName,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPRequestMethodKey.String(c.Request.Method),
				semconv.HTTPRouteKey.String(c.FullPath()),
				semconv.URLPathKey.String(c.Request.URL.Path),
				semconv.NetworkPeerAddressKey.String(c.ClientIP()),
				attribute.String("http.user_agent", c.Request.UserAgent()),
			),
		)
		defer span.End()

		// 3. Gắn context đã được cập nhật vào request và Gin context
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		// 4. Ghi nhận thông tin response sau khi handler thực thi xong
		statusCode := c.Writer.Status()
		span.SetAttributes(semconv.HTTPResponseStatusCodeKey.Int(statusCode))

		if statusCode >= 500 {
			span.SetStatus(1, "HTTP 5xx error") // codes.Error = 1
		}
	}
}
