package setting

import "testing"

func TestSetDefaults(t *testing.T) {
	Conf = &AppConfig{}
	setDefaults()

	if Conf.KafkaConfig == nil || len(Conf.KafkaConfig.Brokers) == 0 || Conf.KafkaConfig.GroupID == "" {
		t.Fatalf("kafka defaults not applied")
	}
	if Conf.NSQConfig == nil || Conf.NSQConfig.Addr == "" {
		t.Fatalf("nsq defaults not applied")
	}
	if Conf.AuthConfig == nil || Conf.AuthConfig.JWTSecret == "" {
		t.Fatalf("auth defaults not applied")
	}
	if Conf.SecurityConfig == nil || Conf.SecurityConfig.RateLimitPerMinute <= 0 {
		t.Fatalf("security defaults not applied")
	}
}

func TestValidate(t *testing.T) {
	Conf = &AppConfig{
		Port: 8080,
		MySQLConfig: &MySQLConfig{
			Host: "127.0.0.1",
			User: "root",
			DB:   "goblog",
			Port: 3306,
		},
		RedisConfig: &RedisConfig{
			Host: "127.0.0.1",
			Port: 6379,
		},
		AuthConfig: &AuthConfig{
			JWTSecret: "secret",
		},
		MessageQueueConfig: &MessageQueueConfig{
			KafkaConfig: &KafkaConfig{Enabled: true, Brokers: []string{"127.0.0.1:9092"}, GroupID: "test"},
			NSQConfig:   &NSQConfig{Enabled: false},
		},
		SecurityConfig: &SecurityConfig{
			RateLimitPerMinute: 60,
			RateLimitBurst:     10,
		},
	}

	if err := validate(); err != nil {
		t.Fatalf("validate should pass, got error: %v", err)
	}

	Conf.AuthConfig.JWTSecret = ""
	if err := validate(); err == nil {
		t.Fatalf("validate should fail when jwt secret is empty")
	}
}
