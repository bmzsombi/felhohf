package kafka

import (
	"context"
	"log"
	"sync"
	"time"

	kafka "github.com/segmentio/kafka-go"
)

const (
	kafkaBrokerAddress = "my-kafka:9092"
	imageUploadTopic   = "image-upload"
	consumerGroupID    = "my-groupid"
)

type MyKafka struct {
	kafkaProducer *kafka.Writer
	kafkaConsumer *kafka.Reader
	uploadTopic   string
	consumerGroup string
	wg            sync.WaitGroup
}

func (mk *MyKafka) InitProducer() error {
	mk.kafkaProducer = &kafka.Writer{
		Addr:      kafka.TCP(kafkaBrokerAddress),
		Topic:     imageUploadTopic,
		Balancer:  &kafka.RoundRobin{},
		Transport: &kafka.Transport{}, // Explicit transport initialization
	}
	log.Println("Kafka producer inicializálva")
	return nil
}

func (mk *MyKafka) SendMessage(ctx context.Context, key, value []byte) error {
	if mk.kafkaProducer == nil {
		if err := mk.InitProducer(); err != nil {
			return err
		}
	}

	message := kafka.Message{
		Key:   key,
		Value: value,
	}

	err := mk.kafkaProducer.WriteMessages(ctx, message)
	if err != nil {
		log.Printf("Hiba az üzenet küldésekor a Kafka-ba: %v", err)
		return err
	}

	return nil
}

func (mk *MyKafka) CloseProducerCloseConsumer() {
	if mk.kafkaProducer != nil {
		if err := mk.kafkaProducer.Close(); err != nil {
			log.Printf("Hiba a Kafka producer lezárásakor: %v", err)
		} else {
			log.Println("Kafka producer lezárva")
		}
	}
	if mk.kafkaConsumer != nil {
		if err := mk.kafkaConsumer.Close(); err != nil {
			log.Printf("Hiba a Kafka consumer lezárásakor: %v", err)
		} else {
			log.Println("Kafka consumer lezárva")
		}
	}
	mk.wg.Wait()
}

func (mk *MyKafka) InitConsumer() error {
	if mk.kafkaConsumer != nil {
		return nil
	}

	dialer := &kafka.Dialer{
		Timeout:   30 * time.Second, // Példa timeout
		DualStack: true,
		// Itt lehet további dialer beállításokat megadni, ha szükséges
	}

	mk.kafkaConsumer = kafka.NewReader(kafka.ReaderConfig{
		Brokers:     []string{kafkaBrokerAddress},
		GroupID:     consumerGroupID,
		Topic:       imageUploadTopic,
		StartOffset: kafka.FirstOffset, // Honnan kezdje olvasni
		Dialer:      dialer,
	})
	mk.consumerGroup = consumerGroupID

	log.Println("Kafka consumer inicializálva")
	return nil
}

func (mk *MyKafka) ConsumeMessages(ctx context.Context, messageHandler func(key, value []byte) error) {
	if mk.kafkaConsumer == nil {
		if err := mk.InitConsumer(); err != nil {
			log.Printf("Nem sikerült inicializálni a Consumert, ezért nem indítom el a consume loop-ot")
			return
		}
	}

	mk.wg.Add(1)
	defer mk.wg.Done()

	for {
		select {
		case <-ctx.Done():
			log.Println("ConsumeMessages leállítva a kontextus miatt.")
			return
		default:
			msg, err := mk.kafkaConsumer.ReadMessage(ctx)
			if err != nil {
				if err == context.Canceled {
					return
				}
				log.Printf("Hiba üzenet olvasásakor Kafka-ból: %v", err)
				continue
			}

			/*
				log.Printf("Üzenet érkezett: Topic: %s, Partition: %d, Offset: %d, Key: %s, Value: %s",
					msg.Topic, msg.Partition, msg.Offset, string(msg.Key), string(msg.Value))
			*/
			if err := messageHandler(msg.Key, msg.Value); err != nil {
				log.Printf("Hiba az üzenet feldolgozásakor: %v", err)
			} else {
				if err := mk.kafkaConsumer.CommitMessages(ctx, msg); err != nil {
					log.Printf("Hiba az offset commitálásakor: %v", err)
				}
			}
		}
	}
}
