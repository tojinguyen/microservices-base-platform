package domain

type EventType string

const (
	EventOTP        EventType = "auth_otp"
	EventLoginAlert EventType = "auth_login_alert"

	EventOrderCreated   EventType = "order_created"
	EventPaymentSuccess EventType = "payment_success"

	EventPromotion EventType = "promotion_campaign"
)

type NotificationChannel string

const (
	ChannelEmail NotificationChannel = "email"
	ChannelSMS   NotificationChannel = "sms"
	ChannelZalo  NotificationChannel = "zalo"
	ChannelPush  NotificationChannel = "push"
)

type NotificationStatus string

const (
	NotificationStatusPending    NotificationStatus = "pending"
	NotificationStatusProcessing NotificationStatus = "processing"
	NotificationStatusSent       NotificationStatus = "sent"
	NotificationStatusDelivering NotificationStatus = "delivering"
	NotificationStatusFailed     NotificationStatus = "failed"
)
