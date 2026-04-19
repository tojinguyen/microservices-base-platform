package service

import (
	"context"
	"log"

	"github.com/tojinguyen/notification/internal/domain"
)

func (s *notificationService) SeedTemplates(ctx context.Context) error {
	templates := []domain.NotificationTemplate{
		{
			EventType: domain.EventOTP,
			Channel:   domain.ChannelEmail,
			Subject:   "Your OTP Verification Code",
			Content:   "Hello, your OTP code is: {{.otp}}. This code is valid for 5 minutes.",
		},
		{
			EventType: domain.EventLoginAlert,
			Channel:   domain.ChannelEmail,
			Subject:   "New Login Security Alert",
			Content:   "Your account was just logged in from device {{.device}} at {{.location}}. If this wasn't you, please secure your account immediately.",
		},
		{
			EventType: domain.EventOrderCreated,
			Channel:   domain.ChannelEmail,
			Subject:   "Order Confirmation - #{{.order_id}}",
			Content:   "Hi {{.name}}, your order #{{.order_id}} has been successfully placed and is currently being processed.",
		},
		{
			EventType: domain.EventPaymentSuccess,
			Channel:   domain.ChannelEmail,
			Subject:   "Payment Successful - Order #{{.order_id}}",
			Content:   "Thank you {{.name}}, your payment of {{.amount}} {{.currency}} for order #{{.order_id}} has been processed successfully.",
		},
		{
			EventType: domain.EventPromotion,
			Channel:   domain.ChannelEmail,
			Subject:   "Exclusive Offer: {{.campaign_name}}",
			Content:   "Hello {{.name}}, you've just received a special discount! Use code {{.discount_code}} to get {{.discount_value}} off your next purchase.",
		},
	}

	for _, t := range templates {
		// Check if already exists
		existing, _ := s.templateRepo.GetByEventType(ctx, t.EventType)
		if existing != nil {
			continue
		}

		log.Printf("Seeding template for event: %s", t.EventType)
		if err := s.templateRepo.Create(ctx, &t); err != nil {
			log.Printf("Failed to seed template %s: %v", t.EventType, err)
			return err
		}
	}

	return nil
}
