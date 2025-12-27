package config

import (
	"fmt"
	"log"
	"strings"

	"github.com/spf13/viper"
)

type Postgres struct {
	Host     string `mapstructure:"host"`
	Port     string `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DBName   string `mapstructure:"database"`
	SSLMode  string `mapstructure:"sslmode"`
}

func (p Postgres) ConnStr() string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s", p.Host, p.User, p.Password, p.DBName, p.Port, p.SSLMode)
}

func (p Postgres) ReplicationConnStr() string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s replication=database", p.Host, p.User, p.Password, p.DBName, p.Port, p.SSLMode)
}

type Nats struct {
	Host               string `mapstructure:"host"`
	Port               string `mapstructure:"port"`
	Stream             string `mapstructure:"stream"`
	RestaurantsSubject string `mapstructure:"restaurantsSubject"`
	MenuItemsSubject   string `mapstructure:"menuItemsSubject"`
	CategoriesSubject  string `mapstructure:"categoriesSubject"`
}

func (n Nats) ConnStr() string {
	return fmt.Sprintf("nats://%s:%s", n.Host, n.Port)
}

type Replication struct {
	Name string `mapstructure:"name"`
	Slot string `mapstructure:"slot"`
}
type Ollama struct {
	Host           string `mapstructure:"host"`
	Port           string `mapstructure:"port"`
	EmbeddingModel string `mapstructure:"embeddingModel"`
	ContextModel   string `mapstructure:"contextModel"`
	ParserModel    string `mapstructure:"parserModel"`
}

func (o *Ollama) Address() string {
	return fmt.Sprintf("http://%s:%s", o.Host, o.Port)
}

type Server struct {
	Port int    `mapstructure:"port"`
	Host string `mapstructure:"host"`
}

func (s *Server) Address() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

type Embedder struct {
	Workers   int `mapstructure:"workers"`
	QueueSize int `mapstructure:"queueSize"`
}

type Config struct {
	Postgres    Postgres    `mapstructure:"postgres"`
	Nats        Nats        `mapstructure:"nats"`
	Ollama      Ollama      `mapstructure:"ollama"`
	Replication Replication `mapstructure:"replication"`
	Server      Server      `mapstructure:"server"`
	Embedder    Embedder    `mapstructure:"embedder"`
}

func LoadConfig() *Config {
	viper.SetConfigFile("./config/config.yaml")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := viper.ReadInConfig(); err != nil {
		log.Fatal(err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		log.Fatal(err)
	}

	return &config
}
