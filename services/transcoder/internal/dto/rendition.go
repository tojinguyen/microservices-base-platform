package dto

type RenditionConfig struct {
	Name          string
	Width         int
	Height        int
	VideoBitrateK int
	MaxRateK      int
	BufSizeK      int
	AudioBitrateK int
	AudioChannels int
	AudioOnly     bool
}

var DefaultRenditions = []RenditionConfig{
	{Name: "360p", Width: 640, Height: 360, VideoBitrateK: 500, MaxRateK: 535, BufSizeK: 1000, AudioBitrateK: 96, AudioChannels: 2},
	{Name: "720p", Width: 1280, Height: 720, VideoBitrateK: 2500, MaxRateK: 2675, BufSizeK: 5000, AudioBitrateK: 128, AudioChannels: 2},
	{Name: "1080p", Width: 1920, Height: 1080, VideoBitrateK: 5000, MaxRateK: 5350, BufSizeK: 10000, AudioBitrateK: 192, AudioChannels: 2},
	{Name: "audio", Width: 0, Height: 0, VideoBitrateK: 0, MaxRateK: 0, BufSizeK: 0, AudioBitrateK: 128, AudioChannels: 2, AudioOnly: true},
}
