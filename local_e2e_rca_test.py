#!/usr/bin/env python3
"""
LagRadar End-to-End RCA Pipeline Test Script
Tests various consumer lag scenarios to trigger different RCA event types
"""

import json
import time
import threading
import logging
import argparse
from enum import Enum
from typing import Dict, List, Optional
import requests
from confluent_kafka import Producer, Consumer, KafkaError
from confluent_kafka.admin import AdminClient, NewTopic
import redis

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

# Global configuration
KAFKA_BOOTSTRAP_SERVERS = 'localhost:9092'

class EventType(Enum):
    """RCA Event Types from events.go"""
    CONSUMER_STALLED = "consumer_stalled"
    RAPID_LAG_INCREASE = "rapid_lag_increase"
    CONSUMER_RECOVERED = "consumer_recovered"
    LAG_CLEARED = "lag_cleared"


class TestScenario:
    """Base class for test scenarios"""

    def __init__(self, name: str, topic: str, group: str, partition: int = 0):
        self.name = name
        self.topic = topic
        self.group = group
        self.partition = partition
        self.producer_thread = None
        self.consumer_thread = None
        self.stop_event = threading.Event()
        self.scenario_complete = threading.Event()

    def start(self):
        """Start the test scenario"""
        logger.info(f"Starting scenario: {self.name}")
        self.producer_thread = threading.Thread(target=self.producer_logic, name=f"{self.name}-producer")
        self.consumer_thread = threading.Thread(target=self.consumer_logic, name=f"{self.name}-consumer")

        self.producer_thread.start()
        self.consumer_thread.start()

    def stop(self):
        """Stop the test scenario"""
        logger.info(f"Stopping scenario: {self.name}")
        self.stop_event.set()

        if self.producer_thread:
            self.producer_thread.join()
        if self.consumer_thread:
            self.consumer_thread.join()

    def producer_logic(self):
        raise NotImplementedError

    def consumer_logic(self):
        raise NotImplementedError

    def wait_for_completion(self, timeout: int = 300):
        return self.scenario_complete.wait(timeout)


class ConsumerStalledScenario(TestScenario):
    """Scenario: Consumer processes messages very slowly"""

    def producer_logic(self):
        conf = {
            'bootstrap.servers': 'localhost:9092',
            'client.id': f'{self.name}-producer'
        }

        producer = Producer(conf)

        message_count = 0
        while not self.stop_event.is_set() and message_count < 2000:
            try:
                value = json.dumps({'id': message_count, 'timestamp': time.time()})
                producer.produce(self.topic, value=value.encode('utf-8'))
                message_count += 1

                if message_count % 200 == 0:
                    logger.info(f"[{self.name}] Produced {message_count} messages")
                    producer.flush()

                time.sleep(0.005)  # 200 msg/sec

            except Exception as e:
                logger.error(f"Producer error: {e}")

        producer.flush()
        logger.info(f"[{self.name}] Producer finished, sent {message_count} messages")

    def consumer_logic(self):
        conf = {
            'bootstrap.servers': 'localhost:9092',
            'group.id': self.group,
            'auto.offset.reset': 'earliest',
            'enable.auto.commit': True,
            'auto.commit.interval.ms': 5000
        }

        consumer = Consumer(conf)
        consumer.subscribe([self.topic])

        messages_consumed = 0

        while not self.stop_event.is_set():
            try:
                msg = consumer.poll(timeout=1.0)

                if msg is None:
                    continue

                if msg.error():
                    if msg.error().code() == KafkaError._PARTITION_EOF:
                        continue
                    else:
                        logger.error(f"Consumer error: {msg.error()}")
                        break

                # Simulate slow processing
                time.sleep(0.1)
                messages_consumed += 1

                if messages_consumed % 50 == 0:
                    logger.info(f"[{self.name}] Slowly consumed {messages_consumed} messages")

                if messages_consumed >= 200:
                    self.scenario_complete.set()
                    consumer.close()
                    return

            except Exception as e:
                logger.error(f"Consumer error: {e}")

        consumer.close()


class RapidLagIncreaseScenario(TestScenario):
    """Scenario: Sudden spike in production rate"""

    def producer_logic(self):
        conf = {
            'bootstrap.servers': 'localhost:9092',
            'client.id': f'{self.name}-producer',
            'linger.ms': 0,  # Send immediately
            'batch.size': 1000
        }

        producer = Producer(conf)

        message_count = 0
        phase = 1

        while not self.stop_event.is_set():
            try:
                # Phase 1: Normal rate
                if phase == 1 and message_count < 500:
                    value = json.dumps({'id': message_count, 'timestamp': time.time()})
                    producer.produce(self.topic, value=value.encode('utf-8'))
                    message_count += 1
                    time.sleep(0.02)

                # Phase 2: Spike in rate
                elif phase == 1 and message_count >= 500:
                    phase = 2
                    logger.info(f"[{self.name}] Entering spike phase")

                elif phase == 2 and message_count < 3000:
                    for _ in range(10):
                        value = json.dumps({'id': message_count, 'timestamp': time.time()})
                        producer.produce(self.topic, value=value.encode('utf-8'))
                        message_count += 1

                    if message_count % 500 == 0:
                        logger.info(f"[{self.name}] Spike phase: {message_count} messages")
                        producer.flush()

                    time.sleep(0.01)

                else:
                    break

            except Exception as e:
                logger.error(f"Producer error: {e}")

        producer.flush()
        logger.info(f"[{self.name}] Producer finished, sent {message_count} messages")

    def consumer_logic(self):
        conf = {
            'bootstrap.servers': 'localhost:9092',
            'group.id': self.group,
            'auto.offset.reset': 'earliest',
            'enable.auto.commit': True,
            'auto.commit.interval.ms': 1000
        }

        consumer = Consumer(conf)
        consumer.subscribe([self.topic])

        messages_consumed = 0

        # Consume at steady rate (can't keep up with spike)
        while not self.stop_event.is_set():
            try:
                msg = consumer.poll(timeout=1.0)

                if msg is None:
                    continue

                if msg.error():
                    if msg.error().code() == KafkaError._PARTITION_EOF:
                        continue
                    else:
                        logger.error(f"Consumer error: {msg.error()}")
                        break

                time.sleep(0.02)  # 50 msg/sec - same as initial producer rate
                messages_consumed += 1

                if messages_consumed % 100 == 0:
                    logger.info(f"[{self.name}] Consumed {messages_consumed} messages")

                # Let it run for a while to detect the lag increase
                if messages_consumed >= 1000:
                    time.sleep(60)  # Keep running to show sustained lag
                    self.scenario_complete.set()
                    break

            except Exception as e:
                logger.error(f"Consumer error: {e}")

        consumer.close()


class ConsumerRecoveryScenario(TestScenario):
    """Scenario: Consumer stops then recovers"""

    def producer_logic(self):
        conf = {
            'bootstrap.servers': 'localhost:9092',
            'client.id': f'{self.name}-producer'
        }

        producer = Producer(conf)

        message_count = 0
        while not self.stop_event.is_set() and message_count < 2000:
            try:
                value = json.dumps({'id': message_count, 'timestamp': time.time()})
                producer.produce(self.topic, value=value.encode('utf-8'))
                message_count += 1

                if message_count % 100 == 0:
                    logger.info(f"[{self.name}] Produced {message_count} messages")
                    producer.flush()

                time.sleep(0.02)  # 50 msg/sec

            except Exception as e:
                logger.error(f"Producer error: {e}")

        producer.flush()

    def consumer_logic(self):
        # Phase 1: Normal consumption
        conf = {
            'bootstrap.servers': 'localhost:9092',
            'group.id': self.group,
            'auto.offset.reset': 'earliest',
            'enable.auto.commit': True,
            'auto.commit.interval.ms': 1000
        }

        consumer = Consumer(conf)
        consumer.subscribe([self.topic])

        messages_consumed = 0

        # Consume for 20 seconds
        start_time = time.time()
        while time.time() - start_time < 20:
            try:
                msg = consumer.poll(timeout=1.0)

                if msg is None:
                    continue

                if msg.error():
                    if msg.error().code() == KafkaError._PARTITION_EOF:
                        continue
                    else:
                        logger.error(f"Consumer error: {msg.error()}")
                        break

                messages_consumed += 1

            except Exception as e:
                logger.error(f"Consumer error: {e}")

        logger.info(f"[{self.name}] Phase 1 complete, consumed {messages_consumed} messages")
        consumer.close()

        # Phase 2: Stop for 60 seconds (to trigger STOPPED event)
        logger.info(f"[{self.name}] Consumer stopped, waiting 60 seconds...")
        time.sleep(60)

        # Phase 3: Resume consumption
        logger.info(f"[{self.name}] Consumer recovering...")
        consumer = Consumer(conf)
        consumer.subscribe([self.topic])

        recovery_start = time.time()
        while time.time() - recovery_start < 30:
            try:
                msg = consumer.poll(timeout=1.0)

                if msg is None:
                    continue

                if msg.error():
                    if msg.error().code() == KafkaError._PARTITION_EOF:
                        continue
                    else:
                        logger.error(f"Consumer error: {msg.error()}")
                        break

                messages_consumed += 1

                if messages_consumed % 100 == 0:
                    logger.info(f"[{self.name}] Recovered: {messages_consumed} total messages")

            except Exception as e:
                logger.error(f"Consumer error: {e}")

        self.scenario_complete.set()


class LagClearedScenario(TestScenario):
    """Scenario: Consumer clears significant lag"""

    def producer_logic(self):
        conf = {
            'bootstrap.servers': 'localhost:9092',
            'client.id': f'{self.name}-producer',
            'linger.ms': 0,
            'batch.size': 1000
        }

        producer = Producer(conf)

        # Produce a large batch upfront
        logger.info(f"[{self.name}] Producing large batch...")
        for i in range(5000):
            value = json.dumps({'id': i, 'timestamp': time.time()})
            producer.produce(self.topic, value=value.encode('utf-8'))
            if i % 500 == 0:
                producer.flush()
                logger.info(f"[{self.name}] Produced {i} messages")

        producer.flush()
        logger.info(f"[{self.name}] Producer finished")

    def consumer_logic(self):
        # Wait for messages to accumulate
        time.sleep(10)

        conf = {
            'bootstrap.servers': 'localhost:9092',
            'group.id': self.group,
            'auto.offset.reset': 'earliest',
            'enable.auto.commit': True,
            'auto.commit.interval.ms': 500,
            'fetch.min.bytes': 1000,
            'max.poll.interval.ms': 300000
        }

        consumer = Consumer(conf)
        consumer.subscribe([self.topic])

        messages_consumed = 0
        no_message_count = 0

        # Consume rapidly to clear lag
        while not self.stop_event.is_set():
            try:
                # Poll without waiting
                msg = consumer.poll(timeout=0.1)

                if msg is None:
                    no_message_count += 1
                    # Check if caught up
                    if no_message_count > 10 and messages_consumed > 4000:
                        logger.info(f"[{self.name}] Lag cleared! Total consumed: {messages_consumed}")
                        time.sleep(30)
                        self.scenario_complete.set()
                        break
                    continue

                if msg.error():
                    if msg.error().code() == KafkaError._PARTITION_EOF:
                        continue
                    else:
                        logger.error(f"Consumer error: {msg.error()}")
                        break

                messages_consumed += 1
                no_message_count = 0

                if messages_consumed % 500 == 0:
                    logger.info(f"[{self.name}] Cleared {messages_consumed} messages")

            except Exception as e:
                logger.error(f"Consumer error: {e}")


class RCAMonitor:
    """Monitor for RCA events in Redis"""

    def __init__(self, redis_host='localhost', redis_port=6379):
        self.redis_client = redis.Redis(host=redis_host, port=redis_port, decode_responses=True)
        self.stream_key = 'lagradar:rca:events'
        self.stop_event = threading.Event()
        self.events_detected = {}

    def start_monitoring(self):
        """Start monitoring Redis stream for RCA events"""
        monitor_thread = threading.Thread(target=self._monitor_loop)
        monitor_thread.start()
        return monitor_thread

    def _monitor_loop(self):
        """Monitor Redis stream for events"""
        last_id = '$'

        while not self.stop_event.is_set():
            try:
                # Read new messages from stream
                messages = self.redis_client.xread(
                    {self.stream_key: last_id},
                    block=1000,  # 1 second timeout
                    count=10
                )

                for stream_name, stream_messages in messages:
                    for msg_id, data in stream_messages:
                        last_id = msg_id
                        self._process_event(msg_id, data)

            except Exception as e:
                logger.error(f"Monitor error: {e}")
                time.sleep(1)

    def _process_event(self, msg_id: str, data: dict):
        """Process RCA event"""
        try:
            event_json = data.get('event', '{}')
            event = json.loads(event_json)

            event_type = event.get('type', 'unknown')
            group_id = event.get('consumer_group', 'unknown')
            topic = event.get('topic', 'unknown')
            partition = event.get('partition', 0)
            severity = event.get('severity', 'unknown')

            logger.info(f"🚨 RCA Event Detected: {event_type}")
            logger.info(f"   Group: {group_id}")
            logger.info(f"   Topic: {topic}[{partition}]")
            logger.info(f"   Severity: {severity}")
            logger.info(f"   Message: {event.get('description', 'N/A')}")
            logger.info(f"   Current Lag: {event.get('current_lag', 'N/A')}")
            logger.info(f"   ID: {msg_id}")

            # Track events
            if event_type not in self.events_detected:
                self.events_detected[event_type] = []
            self.events_detected[event_type].append(event)

        except Exception as e:
            logger.error(f"Error processing event: {e}")

    def stop(self):
        """Stop monitoring"""
        self.stop_event.set()

    def get_summary(self) -> Dict[str, List]:
        """Get summary of detected events"""
        return self.events_detected.copy()


class LagRadarStatusChecker:
    """Check LagRadar API status"""

    def __init__(self, api_url='http://localhost:8080'):
        self.api_url = api_url

    def check_group_status(self, group_id: str) -> Optional[dict]:
        """Check status for a specific consumer group"""
        try:
            response = requests.get(f"{self.api_url}/api/v1/status")
            if response.status_code == 200:
                statuses = response.json()
                for status in statuses:
                    if status.get('group') == group_id:
                        logger.debug(f"Found status for {group_id}: {json.dumps(status, indent=2)}")
                        return status
                logger.debug(f"No status found for group {group_id} in {len(statuses)} statuses")
            else:
                logger.error(f"API returned status code: {response.status_code}")
            return None
        except Exception as e:
            logger.error(f"Failed to check status: {e}")
            return None

    def wait_for_window_data(self, group_id: str, min_completeness: float = 50.0, timeout: int = 300):
        """Wait for sufficient window data"""
        start_time = time.time()

        while time.time() - start_time < timeout:
            status = self.check_group_status(group_id)
            if status:
                status_data = status.get('status', {})
                warning_partitions = status_data.get('WarningPartitions') or []
                critical_partitions = status_data.get('CriticalPartitions') or []
                healthy_partitions = status_data.get('HealthyPartitions') or []
                # Check all partition types
                all_partitions = warning_partitions + critical_partitions + healthy_partitions

                if all_partitions:
                    # Get completeness from first partition with data
                    completeness = 0
                    for partition in all_partitions:
                        if isinstance(partition, dict):
                            completeness = partition.get('WindowCompleteness', 0)
                            if completeness > 0:
                                break

                    logger.info(f"Window completeness for {group_id}: {completeness}%")

                    if completeness >= min_completeness:
                        return True
                else:
                    logger.debug(f"No partition data yet for {group_id}")

            time.sleep(10)

        return False


def create_topic_with_admin(topic_name: str):
    """Create Kafka topic using AdminClient"""
    admin_conf = {
        'bootstrap.servers': 'localhost:9092'
    }

    admin_client = AdminClient(admin_conf)

    # Create topic
    topic = NewTopic(topic_name, num_partitions=1, replication_factor=1)

    try:
        fs = admin_client.create_topics([topic])

        # Wait for operation to finish
        for topic, f in fs.items():
            try:
                f.result()  # The result itself is None
                logger.info(f"Created topic: {topic}")
            except Exception as e:
                if 'already exists' in str(e):
                    logger.info(f"Topic {topic} already exists")
                else:
                    logger.error(f"Failed to create topic {topic}: {e}")

    except Exception as e:
        logger.error(f"Failed to create topic: {e}")


def delete_topic_with_admin(topic_name: str):
    """Delete Kafka topic using AdminClient"""
    admin_conf = {
        'bootstrap.servers': 'localhost:9092'
    }

    admin_client = AdminClient(admin_conf)

    try:
        fs = admin_client.delete_topics([topic_name], operation_timeout=30)

        # Wait for operation to finish
        for topic, f in fs.items():
            try:
                f.result()  # The result itself is None
                logger.info(f"Deleted topic: {topic}")
            except Exception as e:
                logger.debug(f"Failed to delete topic {topic}: {e}")

    except Exception as e:
        logger.debug(f"Failed to delete topic: {e}")


def run_scenario(scenario_class: type, scenario_name: str, status_checker: LagRadarStatusChecker):
    """Run a single test scenario"""
    timestamp = int(time.time())
    topic = f"e2e-{scenario_name}-{timestamp}"
    group = f"e2e-{scenario_name}-group-{timestamp}"

    logger.info(f"\n{'='*60}")
    logger.info(f"Running Scenario: {scenario_name}")
    logger.info(f"Topic: {topic}")
    logger.info(f"Group: {group}")
    logger.info(f"{'='*60}")


    # Create topic
    create_topic_with_admin(topic)

    # Create and start scenario
    scenario = scenario_class(scenario_name, topic, group)
    scenario.start()

    # Wait for window data
    logger.info("Waiting for window data to build...")
    if not status_checker.wait_for_window_data(group, min_completeness=80.0):
        logger.warning("Timeout waiting for window data")

    # Additional wait to ensure LagRadar processes the data
    logger.info("Waiting additional 30s for LagRadar to process...")
    time.sleep(30)

    # Wait for scenario completion
    if scenario.wait_for_completion(timeout=300):
        logger.info(f"Scenario {scenario_name} completed successfully")
    else:
        logger.warning(f"Scenario {scenario_name} timed out")

    # Stop scenario
    scenario.stop()

    # Wait a bit more for events to be processed
    time.sleep(30)

    # Check final status
    final_status = status_checker.check_group_status(group)
    if final_status:
        logger.info(f"Final status: {json.dumps(final_status, indent=2)}")

    # Cleanup
    delete_topic_with_admin(topic)

    logger.info(f"Scenario {scenario_name} finished\n")


def main():
    parser = argparse.ArgumentParser(description='LagRadar E2E RCA Pipeline Test')

    parser.add_argument('--kafka-bootstrap', default='localhost:9092',
                        help='Kafka bootstrap servers (default: localhost:9092)')
    parser.add_argument('--redis-host', default='localhost',
                        help='Redis host (default: localhost)')
    parser.add_argument('--redis-port', default=6379, type=int,
                        help='Redis port (default: 6379)')
    parser.add_argument('--lagradar-api', default='http://localhost:8080',
                        help='LagRadar API URL (default: http://localhost:8080)')

    args = parser.parse_args()

    # Update global bootstrap servers
    global KAFKA_BOOTSTRAP_SERVERS
    KAFKA_BOOTSTRAP_SERVERS = args.kafka_bootstrap

    # Start RCA monitor
    monitor = RCAMonitor()
    monitor_thread = monitor.start_monitoring()

    # Status checker
    status_checker = LagRadarStatusChecker()

    # Define scenarios
    scenarios = {
        'consumer-stalled': ConsumerStalledScenario,
        'rapid-lag-increase': RapidLagIncreaseScenario,
        'consumer-recovered': ConsumerRecoveryScenario,
        'lag-cleared': LagClearedScenario
    }

    try:
        # Run all scenarios
        for name, scenario_class in scenarios.items():
            run_scenario(scenario_class, name, status_checker)
            time.sleep(10)

        # Final summary
        logger.info("\n" + "="*60)
        logger.info("TEST SUMMARY")
        logger.info("="*60)

        event_summary = monitor.get_summary()

        for event_type, events in event_summary.items():
            logger.info(f"\n{event_type}: {len(events)} events")
            for event in events[:3]:  # Show first 3 of each type
                logger.info(f"  - Group: {event.get('consumer_group')}, "
                            f"Topic: {event.get('topic')}, "
                            f"Severity: {event.get('severity')}")

        expected_events = [e.value for e in EventType]
        detected_events = set(event_summary.keys())
        missing_events = set(expected_events) - detected_events

        if missing_events:
            logger.warning(f"\nMissing event types: {missing_events}")
        else:
            logger.info(f"\n✅ All event types detected!")

    finally:
        monitor.stop()
        monitor_thread.join()


if __name__ == '__main__':
    main()
