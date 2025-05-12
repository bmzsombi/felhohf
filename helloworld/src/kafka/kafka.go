package kafka

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"time"

	kafkasg "github.com/segmentio/kafka-go" // Átnevezve kafkasg-re az egyértelműség kedvéért
)

const (
	kafkaBrokerAddress = "my-kafka:9092"
	imageUploadTopic   = "image-upload"
	consumerGroupID    = "my-groupid"
)

type MyKafka struct {
	writer *kafkasg.Writer
	reader *kafkasg.Reader
	wg     sync.WaitGroup
}

// NewMyKafka létrehoz egy új MyKafka példányt.
func (mk *MyKafka) NewMyKafka() *MyKafka {
	return &MyKafka{}
}

// InitWriter inicializálja a Kafka writert (producer).
func (mk *MyKafka) InitWriter() error {
	if mk.writer != nil {
		return nil // Már inicializálva
	}

	// A writer konfigurálása
	// További opciókért lásd: https://pkg.go.dev/github.com/segmentio/kafka-go#WriterConfig
	w := kafkasg.NewWriter(kafkasg.WriterConfig{
		Brokers: []string{kafkaBrokerAddress},
		Topic:   imageUploadTopic, // Alapértelmezett téma ehhez a writerhez
		// Balancer: &kafkasg.LeastBytes{}, // Példa: LeastBytes balancer használata
		// RequiredAcks: kafkasg.RequireAll, // Példa: Várakozás minden replikára
		// Async: false, // Alapértelmezetten false (szinkron)
		Logger:      kafkasg.LoggerFunc(func(format string, args ...interface{}) { log.Printf("KAFKA-WRITER-INFO: "+format, args...) }),
		ErrorLogger: kafkasg.LoggerFunc(func(format string, args ...interface{}) { log.Printf("KAFKA-WRITER-ERROR: "+format, args...) }),
		MaxAttempts: 3, // Üzenetküldési kísérletek száma hiba esetén
	})

	mk.writer = w
	log.Println("Kafka writer (producer) inicializálva a következő témához:", imageUploadTopic)
	return nil
}

// SendMessage üzenetet küld a konfigurált Kafka témába.
func (mk *MyKafka) SendMessage(ctx context.Context, key, value []byte) error {
	if mk.writer == nil {
		if err := mk.InitWriter(); err != nil {
			log.Printf("Nem sikerült inicializálni a Kafka writert: %v", err)
			return err
		}
	}

	message := kafkasg.Message{
		// Topic: Itt felülírható a writer alapértelmezett témája, vagy ha a writernek nincs alapértelmezett témája
		Key:   key,
		Value: value,
		// Time: time.Now(), // Opcionális: üzenet időbélyegének beállítása
	}

	// A WriteMessages alapértelmezetten szinkron
	err := mk.writer.WriteMessages(ctx, message)
	if err != nil {
		log.Printf("Hiba az üzenet Kafka-ba küldésekor: %v", err)
		return err
	}
	// log.Printf("Üzenet sikeresen elküldve a %s témába", mk.writer.Stats().Topic) // A Stats() adhat információt
	return nil
}

// CloseWriterReader lezárja a Kafka writert és readert.
func (mk *MyKafka) CloseWriterReader() {
	if mk.writer != nil {
		log.Println("Kafka writer lezárása...")
		if err := mk.writer.Close(); err != nil {
			log.Printf("Hiba a Kafka writer lezárásakor: %v", err)
		} else {
			log.Println("Kafka writer lezárva")
		}
		mk.writer = nil
	}
	if mk.reader != nil {
		log.Println("Kafka reader lezárása...")
		if err := mk.reader.Close(); err != nil {
			log.Printf("Hiba a Kafka reader lezárásakor: %v", err)
		} else {
			log.Println("Kafka reader lezárva")
		}
		mk.reader = nil
	}
	log.Println("Várakozás a háttérfolyamatok befejeződésére...")
	mk.wg.Wait() // Várakozás a ConsumeMessages goroutine befejeződésére, ha el lett indítva
	log.Println("Minden Kafka erőforrás lezárva és a goroutine-ok befejeződtek.")
}

// InitReader inicializálja a Kafka readert (consumer).
func (mk *MyKafka) InitReader() error {
	if mk.reader != nil {
		return nil // Már inicializálva
	}

	// A reader konfigurálása
	// További opciókért lásd: https://pkg.go.dev/github.com/segmentio/kafka-go#ReaderConfig
	r := kafkasg.NewReader(kafkasg.ReaderConfig{
		Brokers:     []string{kafkaBrokerAddress},
		GroupID:     consumerGroupID,
		Topic:       imageUploadTopic,
		MinBytes:    10e3,                // 10KB (Minimum byte-szám, amit a fetch-nek vissza kell adnia)
		MaxBytes:    10e6,                // 10MB (Maximum byte-szám, amit a fetch-nek vissza kell adnia)
		MaxWait:     time.Second * 1,     // Maximális várakozási idő új üzenetekre (ha a MinBytes nem teljesül)
		StartOffset: kafkasg.FirstOffset, // "earliest"-nek felel meg: a legrégebbi offsettől kezd, ha nincs commitált offset a csoportban
		// CommitInterval: 0, // Ha 0, akkor a ReadMessage után manuálisan kell commitálni FetchMessage/CommitMessages használatával. Alapértelmezetten (ha GroupID van) van auto-commit.
		// Dialer: &kafkasg.Dialer{Timeout: 10 * time.Second, DualStack: true}, // Példa Dialer konfiguráció
		Logger:      kafkasg.LoggerFunc(func(format string, args ...interface{}) { log.Printf("KAFKA-READER-INFO: "+format, args...) }),
		ErrorLogger: kafkasg.LoggerFunc(func(format string, args ...interface{}) { log.Printf("KAFKA-READER-ERROR: "+format, args...) }),
	})
	mk.reader = r
	log.Println("Kafka reader (consumer) inicializálva a következő témához:", imageUploadTopic, "és csoporthoz:", consumerGroupID)
	return nil
}

// ConsumeMessages elindítja az üzenetek fogyasztását a Kafka-ból és feldolgozza őket a messageHandler segítségével.
// Ez a függvény blokkoló, és általában egy goroutine-ban kell futtatni.
func (mk *MyKafka) ConsumeMessages(ctx context.Context, messageHandler func(key, value []byte) error) {
	if mk.reader == nil {
		if err := mk.InitReader(); err != nil {
			log.Printf("Nem sikerült inicializálni a Kafka readert, a fogyasztás nem indul el: %v", err)
			return
		}
	}

	mk.wg.Add(1)
	defer mk.wg.Done()

	log.Println("Kafka üzenetfogyasztás indítása...")
	for {
		// A ReadMessage blokkol, amíg egy üzenet elérhetővé nem válik, a kontextus meg nem szakad, vagy hiba nem történik.
		msg, err := mk.reader.ReadMessage(ctx)
		if err != nil {
			// Ha a kontextus megszakadt, a ctx.Err() nem nil lesz.
			// A reader.Close() szintén hibát eredményez a ReadMessage-ben (gyakran io.EOF vagy context.Canceled).
			if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
				log.Printf("A kontextus megszakadt vagy a reader lezárult, a consumer leáll: %v", err)
				return // Kilépés a ciklusból
			}
			// Egyéb hibák kezelése (pl. átmeneti hálózati problémák)
			// A kafka-go néhány hiba esetén automatikusan újrapróbálkozhat.
			log.Printf("Hiba üzenet olvasásakor a Kafka-ból: %v. Újrapróbálkozás...", err)
			// Opcionális: kis késleltetés hozzáadása az újrapróbálkozás előtt, hogy elkerüljük a szoros ciklust tartós hibák esetén
			select {
			case <-time.After(time.Second): // Várakozás egy másodpercet hiba esetén
			case <-ctx.Done(): // Ha közben a kontextus megszakad
				log.Printf("A kontextus megszakadt hiba utáni várakozás közben: %v", ctx.Err())
				return
			}
			continue
		}

		// log.Printf("Üzenet érkezett: Téma: %s, Partíció: %d, Offset: %d, Kulcs: %s, Érték: %s",
		//	 msg.Topic, msg.Partition, msg.Offset, string(msg.Key), string(msg.Value))

		if err := messageHandler(msg.Key, msg.Value); err != nil {
			log.Printf("Hiba az üzenet feldolgozásakor (Kulcs: %s, Offset: %d): %v", string(msg.Key), msg.Offset, err)
			// Hibakezelési stratégia eldöntése: logolás és folytatás, újra feldolgozás, vagy leállítás.
			// Egyelőre csak logolás és folytatás.
			// Az üzenetek automatikusan commitálódnak a ReadMessage által, ha GroupID van beállítva és a CommitInterval nem 0.
		} else {
			// Az üzenet sikeresen feldolgozva.
			// Ha GroupID-t és alapértelmezett CommitInterval-t használunk (vagy egy megadottat), a commit automatikus.
			// Manuális commithoz a segmentio/kafka-go-val (ha az auto-commit le van tiltva vagy több kontrollra van szükség):
			// Először: `m, err := r.FetchMessage(ctx)`
			// Majd sikeres feldolgozás után: `if err := r.CommitMessages(ctx, m); err != nil { log.Fatal(err) }`
			// log.Printf("Üzenet feldolgozva és commitálva (Offset: %d)", msg.Offset)
		}

		// Explicit kontextus ellenőrzés a ciklusban, ha a ReadMessage nagyon hosszú ideig blokkolhatna
		// anélkül, hogy visszatérne, bár a ReadMessage-nek átadott ctx-nek kezelnie kell a megszakítást.
		select {
		case <-ctx.Done():
			log.Println("A ConsumeMessages ciklus kontextus megszakítást észlelt, leállítás.")
			return
		default:
			// Folytatás a következő üzenettel
		}
	}
}

// TestSendMessage egy előre definiált tesztüzenetet küld.
func (mk *MyKafka) TestSendMessage(ctx context.Context) {
	log.Println("Tesztüzenet küldésének kísérlete...")
	err := mk.SendMessage(ctx, []byte("test-key"), []byte("test-value from segmentio/kafka-go"))
	if err != nil {
		log.Printf("Nem sikerült a tesztüzenet küldése: %v", err)
	} else {
		log.Println("Tesztüzenet sikeresen elküldve.")
	}
}

/*
// Példa használat (koncepcionális):

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Helyettesítsd be a kafka csomagod elérési útjával
	// pl. "yourapp/internal/kafka"
	"yourproject/kafka"
)


func main() {
	// Inicializáljuk a Kafka klienst a csomagunkból
	kafkaClient := kafka.NewMyKafka() // Feltételezve, hogy a MyKafka és a NewMyKafka a "kafka" csomagban van

	// Hozzunk létre egy kontextust, amit le tudunk állítani
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Biztosítsuk, hogy a cancel() meghívódik a main végén

	// Inicializáljuk a Writert (Producer)
	if err := kafkaClient.InitWriter(); err != nil {
		log.Fatalf("Nem sikerült inicializálni a Kafka writert: %v", err)
	}
	// A CloseWriterReader hívását defer-rel biztosítjuk, hogy az erőforrások felszabaduljanak
	// A cancel() után kell meghívni, hogy a consumernek legyen ideje leállni
	defer kafkaClient.CloseWriterReader()

	// Küldjünk egy teszt üzenetet
	kafkaClient.TestSendMessage(ctx)

	// Küldjünk egy másik üzenetet
	err := kafkaClient.SendMessage(ctx, []byte("user-456"), []byte("{\"event\":\"item_purchased\", \"itemId\":\"xyz789\"}"))
	if err != nil {
		log.Printf("Hiba egyedi üzenet küldésekor: %v", err)
	} else {
		log.Println("Egyedi üzenet sikeresen elküldve.")
	}

	// Inicializáljuk a Readert (Consumer)
	if err := kafkaClient.InitReader(); err != nil {
		log.Fatalf("Nem sikerült inicializálni a Kafka readert: %v", err)
	}

	// Indítsuk el az üzenetek fogyasztását egy új goroutine-ban
	log.Println("Consumer indítása egy új goroutine-ban...")
	go func() {
		kafkaClient.ConsumeMessages(ctx, func(key, value []byte) error {
			log.Printf("Handler kapott üzenetet - Kulcs: %s, Érték: %s\n", string(key), string(value))
			// Szimuláljuk a feldolgozást
			time.Sleep(50 * time.Millisecond)
			// nil-t adjunk vissza sikeres feldolgozás esetén, vagy hibát a hiba jelzésére
			return nil
		})
		log.Println("Consumer goroutine befejeződött.")
	}()

	// Tartsuk életben a fő goroutine-t egy ideig, hogy a consumer dolgozhasson
	// Egy valós alkalmazásban ezt az alkalmazás életciklusa kezelné
	log.Println("Consumer elindult. Nyomj Ctrl+C-t a kilépéshez.")

	// Várjunk egy leállítási jelre (pl. SIGINT, SIGTERM)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan // Blokkolás, amíg egy jel meg nem érkezik

	log.Println("Leállítási jel érkezett. Kafka kliens lezárása...")
	cancel() // Jelezzük a consumernek és más műveleteknek, hogy álljanak le

	// A CloseWriterReader a defer miatt itt fog lefutni, megvárva a wg.Wait()-et.
	// De explicit hívás is rendben van, ha a defer nem lenne a cancel() *után*.
	// Ebben a struktúrában a defer a helyes.
	log.Println("Alkalmazás szabályosan leállt.")
}
*/
