package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"os"
	"time"
)

func main() {
	var brokers string
	var groupFilter string

	flag.StringVar(&brokers, "brokers", "localhost:9092", "Kafka broker addresses")
	flag.StringVar(&groupFilter, "group", "", "Specific consumer group to monitor (empty for all groups)")
	flag.Parse()

	admin, err := kafka.NewAdminClient(&kafka.ConfigMap{"bootstrap.servers": brokers})
	if err != nil {
		panic(err)
	}
	defer admin.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	groupsResult, err := admin.ListConsumerGroups(ctx)
	if err != nil {
		panic(err)
	}

	var groupIDs []string

	if groupFilter != "" {

		groupIDs = []string{groupFilter}
		fmt.Printf("Monitoring specific consumer group: %s\n\n", groupFilter)
	} else {

		for _, group := range groupsResult.Valid {
			groupIDs = append(groupIDs, group.GroupID)
		}
		fmt.Printf("Monitoring all %d consumer groups\n\n", len(groupIDs))
	}

	if len(groupIDs) == 0 {
		fmt.Println("No consumer groups found")
		return
	}

	consumer, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers": brokers,
		"group.id":          "lagradar_tmp",
		"auto.offset.reset": "earliest",
	})
	if err != nil {
		panic(err)
	}
	defer consumer.Close()

	fmt.Printf("%-35s %-25s %-10s %-15s %-15s %-10s\n", "GROUP", "TOPIC", "PARTITION", "LOGEND", "COMMITTED", "LAG")
	fmt.Println("------------------------------------------------------------------------------------------------")

	for _, groupID := range groupIDs {

		offsetsResult, err := admin.ListConsumerGroupOffsets(ctx, []kafka.ConsumerGroupTopicPartitions{
			{
				Group: groupID,
			},
		})

		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing offsets for group %s: %v\n", groupID, err)
			continue
		}

		if len(offsetsResult.ConsumerGroupsTopicPartitions) == 0 {
			continue
		}

		groupOffsets := offsetsResult.ConsumerGroupsTopicPartitions[0]

		for _, topicPartition := range groupOffsets.Partitions {
			if topicPartition.Topic == nil {
				continue
			}

			topic := *topicPartition.Topic
			partition := topicPartition.Partition

			_, logend, err := consumer.QueryWatermarkOffsets(topic, partition, 5000)
			if err != nil {
				fmt.Fprintf(os.Stderr, "QueryWatermarkOffsets error for %s[%d]: %v\n", topic, partition, err)
				continue
			}

			committedOffset := topicPartition.Offset

			if committedOffset == kafka.OffsetInvalid {
				fmt.Printf("%-35s %-25s %-10d %-15d %-15s %-10s\n",
					groupID, topic, partition, logend, "NO_COMMIT", "N/A")
				continue
			}

			lag := int64(logend) - int64(committedOffset)
			if lag < 0 {
				lag = 0
			}

			fmt.Printf("%-35s %-25s %-10d %-15d %-15d %-10d\n",
				groupID, topic, partition, logend, committedOffset, lag)
		}
	}
}
