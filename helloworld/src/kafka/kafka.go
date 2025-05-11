package kafka

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/confluentinc/confluent-kafka-go/kafka"
)

const (
	kafkaBrokerAddress = "my-kafka:9092"
	imageUploadTopic   = "image-upload"
	consumerGroupID    = "my-groupid"
)

type MyKafka struct {
	kafkaProducer *kafka.Producer
	kafkaConsumer *kafka.Consumer
	uploadTopic   string
	consumerGroup string
	wg            sync.WaitGroup
}

var uploadTopic = imageUploadTopic

func (mk *MyKafka) InitProducer() error {
	configMap := &kafka.ConfigMap{
		"bootstrap.servers": kafkaBrokerAddress,
		// További producer konfigurációk itt megadhatók
	}

	producer, err := kafka.NewProducer(configMap)
	if err != nil {
		log.Printf("Nem sikerült létrehozni a Kafka producert: %v", err)
		return err
	}
	mk.kafkaProducer = producer
	log.Println("Kafka producer inicializálva")
	return nil
}

func (mk *MyKafka) SendMessage(ctx context.Context, key, value []byte) error {
	if mk.kafkaProducer == nil {
		if err := mk.InitProducer(); err != nil {
			return err
		}
	}

	deliveryChan := make(chan kafka.Event)
	defer close(deliveryChan)

	message := &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &uploadTopic, Partition: kafka.PartitionAny},
		Key:            key,
		Value:          value,
	}

	err := mk.kafkaProducer.Produce(message, deliveryChan)
	if err != nil {
		log.Printf("Hiba az üzenet küldésekor a Kafka-ba: %v", err)
		return err
	}

	// Várakozás a kézbesítési jelentésre
	e := <-deliveryChan
	switch ev := e.(type) {
	case *kafka.Message:
		if ev.TopicPartition.Error != nil {
			log.Printf("Üzenet kézbesítési hiba: %v\n", ev.TopicPartition.Error)
			return ev.TopicPartition.Error
		} else {
			// Sikeres kézbesítés esetén (opcionális logolás)
			// log.Printf("Üzenet sikeresen kézbesítve a %s [%d] @ %v címre\n",
			// 	*ev.TopicPartition.Topic, ev.TopicPartition.Partition, ev.TopicPartition.Offset)
		}
	case kafka.Error:
		log.Printf("Kafka hiba: %v\n", ev)
		return ev
	}

	return nil
}

func (mk *MyKafka) CloseProducerCloseConsumer() {
	if mk.kafkaProducer != nil {
		mk.kafkaProducer.Close()
		log.Println("Kafka producer lezárva")
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

	configMap := &kafka.ConfigMap{
		"bootstrap.servers": kafkaBrokerAddress,
		"group.id":          consumerGroupID,
		"auto.offset.reset": "earliest", // vagy "latest", "none"
		// További consumer konfigurációk itt megadhatók
	}

	consumer, err := kafka.NewConsumer(configMap)
	if err != nil {
		log.Printf("Nem sikerült létrehozni a Kafka consumert: %v", err)
		return err
	}

	err = consumer.SubscribeTopics([]string{imageUploadTopic}, nil)
	if err != nil {
		log.Printf("Nem sikerült feliratkozni a témára: %v", err)
		if closeErr := consumer.Close(); closeErr != nil {
			log.Printf("Hiba a consumer lezárásakor a feliratkozási hiba után: %v", closeErr)
		}
		return err
	}

	mk.kafkaConsumer = consumer
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
			msg, err := mk.kafkaConsumer.ReadMessage(time.Second) // Hozzáadtam egy timeout-ot
			if err != nil {
				if kerr, ok := err.(kafka.Error); ok && kerr.Code() == kafka.ErrTimedOut {
					continue // Nincs üzenet a timeout miatt, folytatjuk a loop-ot
				}
				log.Printf("Hiba üzenet olvasásakor Kafka-ból: %v", err)
				continue
			}

			/*
				log.Printf("Üzenet érkezett: Topic: %s, Partition: %d, Offset: %v, Key: %s, Value: %s",
					msg.TopicPartition.Topic, msg.TopicPartition.Partition, msg.TopicPartition.Offset, string(msg.Key), string(msg.Value))
			*/
			if err := messageHandler(msg.Key, msg.Value); err != nil {
				log.Printf("Hiba az üzenet feldolgozásakor: %v", err)
			} else {
				// Az offset commitálása automatikusan történik, ha az "enable.auto.commit" true (alapértelmezett).
				// Ha manuálisan szeretnénk commitálni, állítsuk "enable.auto.commit"-ot false-ra a konfigurációban
				// és használjuk a consumer.CommitMessage(msg) metódust.
			}
		}
	}
}

func (mk *MyKafka) TestSendMessage(ctx context.Context) {
	if mk.kafkaProducer == nil {
		if err := mk.InitProducer(); err != nil {
			log.Printf("Nem sikerült inicializálni a producert a tesztüzenet küldéséhez: %v", err)
			return
		}
	}

	deliveryChan := make(chan kafka.Event)
	defer close(deliveryChan)

	message := &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &uploadTopic, Partition: kafka.PartitionAny},
		Key:            []byte("test-key"),
		Value:          []byte("test-value"),
	}

	err := mk.kafkaProducer.Produce(message, deliveryChan)
	if err != nil {
		log.Printf("Hiba tesztüzenet küldésekor a Kafka-ba: %v", err)
	} else {
		e := <-deliveryChan
		switch ev := e.(type) {
		case *kafka.Message:
			if ev.TopicPartition.Error != nil {
				log.Printf("Tesztüzenet kézbesítési hiba: %v\n", ev.TopicPartition.Error)
			} else {
				log.Println("Tesztüzenet sikeresen elküldve")
			}
		case kafka.Error:
			log.Printf("Kafka hiba a tesztüzenet küldésekor: %v\n", ev)
		}
	}
}
