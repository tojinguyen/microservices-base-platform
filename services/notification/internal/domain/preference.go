package domain

type NotificationPreference struct {
	BaseModel
	UserID    string              `gorm:"size:100;not null;index:idx_pref_user_event_channel,unique"`
	EventType EventType           `gorm:"size:100;not null;index:idx_pref_user_event_channel,unique"`
	Channel   NotificationChannel `gorm:"size:20;not null;index:idx_pref_user_event_channel,unique"`
	Enabled   bool                `gorm:"default:true;not null"`
}
