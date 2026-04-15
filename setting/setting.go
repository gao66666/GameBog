package setting

import (
	"fmt"
	"os"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

var Conf = new(AppConfig)

type AppConfig struct {
	Name      string `mapstructure:"name"`
	Mode      string `mapstructure:"mode"`
	Version   string `mapstructure:"version"`
	StartTime string `mapstructure:"start_time"`
	MachineID int64  `mapstructure:"machine_id"`
	Port      int    `mapstructure:"port"`

	*LogConfig           `mapstructure:"log"`
	*MySQLConfig         `mapstructure:"mysql"`
	*RedisConfig         `mapstructure:"redis"`
	*AuthConfig          `mapstructure:"auth"`
	*MessageQueueConfig  `mapstructure:"mq"`
	*SearchConfig        `mapstructure:"search"`
	*SecurityConfig      `mapstructure:"security"`
	*ObservabilityConfig `mapstructure:"observability"`
}

type AuthConfig struct {
	JWTExpireHours int    `mapstructure:"jwt_expire"`
	JWTSecret      string `mapstructure:"jwt_secret"`
}

type MySQLConfig struct {
	Host         string `mapstructure:"host"`
	User         string `mapstructure:"user"`
	Password     string `mapstructure:"password"`
	DB           string `mapstructure:"dbname"`
	Port         int    `mapstructure:"port"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

type RedisConfig struct {
	Host         string `mapstructure:"host"`
	Password     string `mapstructure:"password"`
	Port         int    `mapstructure:"port"`
	DB           int    `mapstructure:"db"`
	PoolSize     int    `mapstructure:"pool_size"`
	MinIdleConns int    `mapstructure:"min_idle_conns"`
}

type LogConfig struct {
	Level           string `mapstructure:"level"`
	Common_filename string `mapstructure:"common_filename"`
	Error_filename  string `mapstructure:"error_filename"`
	MaxSize         int    `mapstructure:"max_size"`
	MaxAge          int    `mapstructure:"max_age"`
	MaxBackups      int    `mapstructure:"max_backups"`
}

type MessageQueueConfig struct {
	*KafkaConfig `mapstructure:"kafka"`
	*NSQConfig   `mapstructure:"nsq"`
}

type KafkaConfig struct {
	Enabled bool     `mapstructure:"enabled"`
	Brokers []string `mapstructure:"brokers"`
	GroupID string   `mapstructure:"group_id"`
}

type NSQConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Addr    string `mapstructure:"addr"`
}

type SearchConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Host    string `mapstructure:"host"`
	APIKey  string `mapstructure:"api_key"`
	Index   string `mapstructure:"index"`
}

type SecurityConfig struct {
	RateLimitPerMinute int `mapstructure:"rate_limit_per_minute"`
	RateLimitBurst     int `mapstructure:"rate_limit_burst"`
}

type ObservabilityConfig struct {
	EnableMetrics bool `mapstructure:"enable_metrics"`
}

func Init(filePath string) (err error) {
	// 方式1：直接指定配置文件路径（相对路径或者绝对路径）
	// 相对路径：相对执行的可执行文件的相对路径
	//viper.SetConfigFile("./conf/config.yaml")
	// 绝对路径：系统中实际的文件路径
	//viper.SetConfigFile("/Users/liwenzhou/Desktop/bluebell/conf/config.yaml")

	// 方式2：指定配置文件名和配置文件的位置，viper自行查找可用的配置文件
	// 配置文件名不需要带后缀
	// 配置文件位置可配置多个
	//viper.SetConfigName("config") // 指定配置文件名（不带后缀）
	//viper.AddConfigPath(".") // 指定查找配置文件的路径（这里使用相对路径）
	//viper.AddConfigPath("./conf")      // 指定查找配置文件的路径（这里使用相对路径）

	// 基本上是配合远程配置中心使用的，告诉viper当前的数据使用什么格式去解析
	//viper.SetConfigType("json")

	viper.SetConfigFile(filePath)
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	err = viper.ReadInConfig() // 读取配置信息
	if err != nil {
		// 读取配置信息失败
		fmt.Printf("viper.ReadInConfig failed, err:%v\n", err)
		return
	}

	// 把读取到的配置信息反序列化到 Conf 变量中
	if err := viper.Unmarshal(Conf); err != nil {
		fmt.Printf("viper.Unmarshal failed, err:%v\n", err)
	}

	loadSecretFromEnv()
	setDefaults()
	if err := validate(); err != nil {
		return err
	}

	viper.WatchConfig()
	viper.OnConfigChange(func(in fsnotify.Event) {
		fmt.Println("配置文件修改了...")
		if err := viper.Unmarshal(Conf); err != nil {
			fmt.Printf("viper.Unmarshal failed, err:%v\n", err)
			return
		}
		loadSecretFromEnv()
		setDefaults()
	})
	return
}

func loadSecretFromEnv() {
	if Conf == nil {
		return
	}
	if Conf.SearchConfig == nil {
		Conf.SearchConfig = &SearchConfig{}
	}
	if val := os.Getenv("SEARCH_API_KEY"); val != "" {
		Conf.SearchConfig.APIKey = val
	}
}

func setDefaults() {
	if Conf.MessageQueueConfig == nil {
		Conf.MessageQueueConfig = &MessageQueueConfig{}
	}
	if Conf.KafkaConfig == nil {
		Conf.KafkaConfig = &KafkaConfig{}
	}
	if Conf.NSQConfig == nil {
		Conf.NSQConfig = &NSQConfig{}
	}
	if Conf.SearchConfig == nil {
		Conf.SearchConfig = &SearchConfig{}
	}
	if Conf.AuthConfig == nil {
		Conf.AuthConfig = &AuthConfig{}
	}
	if Conf.SecurityConfig == nil {
		Conf.SecurityConfig = &SecurityConfig{}
	}
	if Conf.ObservabilityConfig == nil {
		Conf.ObservabilityConfig = &ObservabilityConfig{}
	}

	if len(Conf.KafkaConfig.Brokers) == 0 {
		Conf.KafkaConfig.Brokers = []string{"127.0.0.1:9092"}
	}
	if Conf.KafkaConfig.GroupID == "" {
		Conf.KafkaConfig.GroupID = "goblog-workers"
	}
	if Conf.NSQConfig.Addr == "" {
		Conf.NSQConfig.Addr = "127.0.0.1:4150"
	}
	if Conf.SearchConfig.Index == "" {
		Conf.SearchConfig.Index = "articles"
	}
	if Conf.AuthConfig.JWTExpireHours <= 0 {
		Conf.AuthConfig.JWTExpireHours = 24
	}
	if Conf.AuthConfig.JWTSecret == "" {
		Conf.AuthConfig.JWTSecret = "change-me-in-production"
	}
	if Conf.SecurityConfig.RateLimitPerMinute <= 0 {
		Conf.SecurityConfig.RateLimitPerMinute = 120
	}
	if Conf.SecurityConfig.RateLimitBurst <= 0 {
		Conf.SecurityConfig.RateLimitBurst = 20
	}
}

func validate() error {
	if Conf.Port <= 0 {
		return fmt.Errorf("invalid port: %d", Conf.Port)
	}
	if Conf.MySQLConfig == nil || Conf.MySQLConfig.Host == "" || Conf.MySQLConfig.User == "" || Conf.MySQLConfig.DB == "" {
		return fmt.Errorf("mysql config is incomplete")
	}
	if Conf.RedisConfig == nil || Conf.RedisConfig.Host == "" || Conf.RedisConfig.Port <= 0 {
		return fmt.Errorf("redis config is incomplete")
	}
	if Conf.AuthConfig == nil || Conf.AuthConfig.JWTSecret == "" {
		return fmt.Errorf("auth config is incomplete")
	}
	if Conf.KafkaConfig.Enabled && len(Conf.KafkaConfig.Brokers) == 0 {
		return fmt.Errorf("kafka enabled but brokers is empty")
	}
	if Conf.NSQConfig.Enabled && Conf.NSQConfig.Addr == "" {
		return fmt.Errorf("nsq enabled but addr is empty")
	}
	if Conf.SecurityConfig.RateLimitPerMinute <= 0 || Conf.SecurityConfig.RateLimitBurst <= 0 {
		return fmt.Errorf("invalid security rate limit config")
	}
	return nil
}
