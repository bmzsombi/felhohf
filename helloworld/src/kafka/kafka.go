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

func (mk *MyKafka) InitProducer() error {
	var err error
	mk.kafkaProducer, err = kafka.NewProducer(&kafka.ConfigMap{
		"bootstrap.servers": kafkaBrokerAddress,
	})
	if err != nil {
		log.Printf("Hiba a Kafka producer létrehozásakor: %v", err)
		return err
	}

	mk.uploadTopic = imageUploadTopic

	go func() {
		for e := range mk.kafkaProducer.Events() {
			switch ev := e.(type) {
			case *kafka.Message:
				if ev.TopicPartition.Error != nil {
					log.Printf("Hiba a kézbesítés során: %v\n", ev.TopicPartition.Error)
				} else {
					log.Printf("Üzenet kézbesítve a(z) %s [%d] offset %d helyre\n",
						*ev.TopicPartition.Topic, ev.TopicPartition.Partition, ev.TopicPartition.Offset)
				}
			case kafka.Error:
				log.Printf("Producer hiba: %v (%v)\n", ev.Code(), ev)
			}
		}
	}()
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

	err := mk.kafkaProducer.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &mk.uploadTopic, Partition: kafka.PartitionAny},
		Key:            key,
		Value:          value,
	}, deliveryChan)
	if err != nil {
		log.Printf("Hiba az üzenet küldésekor a Kafka-ba: %v", err)
		return err
	}

	e := <-deliveryChan
	m := e.(*kafka.Message)
	if m.TopicPartition.Error != nil {
		log.Printf("Hiba a kézbesítés során: %v\n", m.TopicPartition.Error)
		return m.TopicPartition.Error
	} else {
		log.Printf("Üzenet kézbesítve a(z) %s [%d] offset %d helyre\n",
			*m.TopicPartition.Topic, m.TopicPartition.Partition, m.TopicPartition.Offset)
	}

	close(deliveryChan)

	return nil
}

func (mk *MyKafka) CloseProducerCloseConsumer() {
	if mk.kafkaProducer != nil {
		mk.kafkaProducer.Flush(15 * 1000)
		mk.kafkaProducer.Close()
		log.Println("Kafka producer lezárva")
	}
	if mk.kafkaConsumer != nil {
		mk.kafkaConsumer.Close()
		log.Println("Kafka consumer lezárva")
	}
	mk.wg.Wait() //várunk hogy minden goroutine befejezze a munkát
}

func (mk *MyKafka) InitConsumer() error {
	if mk.kafkaConsumer != nil {
		return nil // Már inicializálva van
	}

	c, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers": kafkaBrokerAddress,
		"group.id":          consumerGroupID, // Használjuk a konstansot
		"auto.offset.reset": "earliest",      // Fontos: honnan kezdje olvasni
		// További consumer konfigurációk.
	})
	if err != nil {
		log.Printf("Hiba a Kafka consumer létrehozásakor: %v", err)
		return err
	}

	mk.kafkaConsumer = c
	mk.consumerGroup = consumerGroupID
	err = mk.kafkaConsumer.SubscribeTopics([]string{imageUploadTopic}, nil) //paraméterként kapja
	if err != nil {
		log.Printf("Hiba a feliratkozás során: %v", err)
		return err
	}

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
			msg, err := mk.kafkaConsumer.ReadMessage(time.Second)
			if err != nil {
				log.Printf("Hiba üzenet olvasásakor Kafka-ból: %v", err)
				if ctx.Err() != nil {
					return
				}
				continue
			}
			/*
				log.Printf("Üzenet érkezett: Topic: %s, Partition: %d, Offset: %d, Key: %s, Value: %s",
					msg.Topic, msg.Partition, msg.Offset, string(msg.Key), string(msg.Value))
			*/
			if err := messageHandler(msg.Key, msg.Value); err != nil {
				log.Printf("Hiba az üzenet feldolgozásakor: %v", err)
			} else {
				_, err := mk.kafkaConsumer.CommitMessage(msg)
				if err != nil {
					log.Printf("Hiba az offset commitálásakor: %v", err)
				}
			}
		}
	}
}
