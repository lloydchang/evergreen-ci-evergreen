package model

import (
	"time"

	"github.com/evergreen-ci/evergreen/apimodels"
	"github.com/evergreen-ci/evergreen/db"
	mgobson "github.com/evergreen-ci/evergreen/db/mgo/bson"
	"github.com/evergreen-ci/utility"
	"github.com/mongodb/anser/bsonutil"
	adb "github.com/mongodb/anser/db"
	"go.mongodb.org/mongo-driver/bson"
)

const (
	TaskLogDB         = "logs"
	TaskLogCollection = "task_logg"
	MessagesPerLog    = 10
)

// a single chunk of a task log
type TaskLog struct {
	Id           string                 `bson:"_id" json:"_id,omitempty"`
	TaskId       string                 `bson:"t_id" json:"t_id"`
	Execution    int                    `bson:"e" json:"e"`
	Timestamp    time.Time              `bson:"ts" json:"ts"`
	MessageCount int                    `bson:"c" json:"c"`
	Messages     []apimodels.LogMessage `bson:"m" json:"m"`
}

func (t *TaskLog) MarshalBSON() ([]byte, error)  { return mgobson.Marshal(t) }
func (t *TaskLog) UnmarshalBSON(in []byte) error { return mgobson.Unmarshal(in, t) }

var (
	// bson fields for the task log struct
	TaskLogIdKey           = bsonutil.MustHaveTag(TaskLog{}, "Id")
	TaskLogTaskIdKey       = bsonutil.MustHaveTag(TaskLog{}, "TaskId")
	TaskLogExecutionKey    = bsonutil.MustHaveTag(TaskLog{}, "Execution")
	TaskLogTimestampKey    = bsonutil.MustHaveTag(TaskLog{}, "Timestamp")
	TaskLogMessageCountKey = bsonutil.MustHaveTag(TaskLog{}, "MessageCount")
	TaskLogMessagesKey     = bsonutil.MustHaveTag(TaskLog{}, "Messages")

	// bson fields for the log message struct
	LogMessageTypeKey      = bsonutil.MustHaveTag(apimodels.LogMessage{}, "Type")
	LogMessageSeverityKey  = bsonutil.MustHaveTag(apimodels.LogMessage{}, "Severity")
	LogMessageMessageKey   = bsonutil.MustHaveTag(apimodels.LogMessage{}, "Message")
	LogMessageTimestampKey = bsonutil.MustHaveTag(apimodels.LogMessage{}, "Timestamp")
)

// helper for getting the correct db
func getSessionAndDB() (adb.Session, adb.Database, error) {
	session, _, err := db.GetGlobalSessionFactory().GetSession()
	if err != nil {
		return nil, nil, err
	}
	return session, session.DB(TaskLogDB), nil
}

/******************************************************
Functions that operate on entire TaskLog documents
******************************************************/

func (tl *TaskLog) Insert() error {
	session, db, err := getSessionAndDB()
	if err != nil {
		return err
	}
	defer session.Close()

	tl.Id = mgobson.NewObjectId().Hex()

	return db.C(TaskLogCollection).Insert(tl)
}

func (tl *TaskLog) AddLogMessage(msg apimodels.LogMessage) error {
	session, db, err := getSessionAndDB()
	if err != nil {
		return err
	}
	defer session.Close()

	// NOTE: this was previously set to fire-and-forget writes,
	// but removed during the database migration

	tl.Messages = append(tl.Messages, msg)
	tl.MessageCount = tl.MessageCount + 1

	return db.C(TaskLogCollection).UpdateId(tl.Id,
		bson.M{
			"$inc": bson.M{
				TaskLogMessageCountKey: 1,
			},
			"$push": bson.M{
				TaskLogMessagesKey: msg,
			},
		},
	)
}

func FindAllTaskLogs(taskId string, execution int) ([]TaskLog, error) {
	session, db, err := getSessionAndDB()
	if err != nil {
		return nil, err
	}
	defer session.Close()

	result := []TaskLog{}
	err = db.C(TaskLogCollection).Find(
		bson.M{
			TaskLogTaskIdKey:    taskId,
			TaskLogExecutionKey: execution,
		},
	).Sort("-" + TaskLogTimestampKey).All(&result)
	if adb.ResultsNotFound(err) {
		return nil, nil
	}
	return result, err
}

func FindMostRecentTaskLogs(taskId string, execution int, limit int) ([]TaskLog, error) {
	session, db, err := getSessionAndDB()
	if err != nil {
		return nil, err
	}
	defer session.Close()

	result := []TaskLog{}
	err = db.C(TaskLogCollection).Find(
		bson.M{
			TaskLogTaskIdKey:    taskId,
			TaskLogExecutionKey: execution,
		},
	).Sort("-" + TaskLogTimestampKey).Limit(limit).All(&result)
	if adb.ResultsNotFound(err) {
		return nil, nil
	}
	return result, err
}

func FindTaskLogsBeforeTime(taskId string, execution int, ts time.Time, limit int) ([]TaskLog, error) {
	session, db, err := getSessionAndDB()
	if err != nil {
		return nil, err
	}
	defer session.Close()

	query := bson.M{
		TaskLogTaskIdKey:    taskId,
		TaskLogExecutionKey: execution,
		TaskLogTimestampKey: bson.M{
			"$lt": ts,
		},
	}

	result := []TaskLog{}
	err = db.C(TaskLogCollection).Find(query).Sort("-" + TaskLogTimestampKey).Limit(limit).All(&result)
	if adb.ResultsNotFound(err) {
		return nil, nil
	}
	return result, err
}

func GetRawTaskLogChannel(taskId string, execution int, severities []string,
	msgTypes []string) (chan apimodels.LogMessage, error) {
	session, db, err := getSessionAndDB()
	if err != nil {
		return nil, err
	}

	logObj := TaskLog{}

	// 100 is an arbitrary magic number. Unbuffered channel would be bad for
	// performance, so just picked a buffer size out of thin air.
	channel := make(chan apimodels.LogMessage, 100)

	var query bson.M
	if execution == 0 {
		query = bson.M{"$and": []bson.M{
			{TaskLogTaskIdKey: taskId},
			{"$or": []bson.M{
				{TaskLogExecutionKey: 0},
				{TaskLogExecutionKey: nil},
			}}}}
	} else {
		query = bson.M{
			TaskLogTaskIdKey:    taskId,
			TaskLogExecutionKey: execution,
		}
	}
	iter := db.C(TaskLogCollection).Find(query).Sort(TaskLogTimestampKey).Iter()

	oldMsgTypes := []string{}
	for _, msgType := range msgTypes {
		switch msgType {
		case apimodels.SystemLogPrefix:
			oldMsgTypes = append(oldMsgTypes, "system")
		case apimodels.AgentLogPrefix:
			oldMsgTypes = append(oldMsgTypes, "agent")
		case apimodels.TaskLogPrefix:
			oldMsgTypes = append(oldMsgTypes, "task")
		}
	}

	go func() {
		defer session.Close()
		defer close(channel)
		defer iter.Close()

		for iter.Next(&logObj) {
			for _, logMsg := range logObj.Messages {
				if len(severities) > 0 &&
					!utility.StringSliceContains(severities, logMsg.Severity) {
					continue
				}
				if len(msgTypes) > 0 {
					if !(utility.StringSliceContains(msgTypes, logMsg.Type) ||
						utility.StringSliceContains(oldMsgTypes, logMsg.Type)) {
						continue
					}
				}
				channel <- logMsg
			}
		}
	}()

	return channel, nil
}

/******************************************************
Functions that operate on individual log messages
******************************************************/

// note: to ignore severity or type filtering, pass in empty slices
func FindMostRecentLogMessages(taskId string, execution int, numMsgs int,
	severities []string, msgTypes []string) ([]apimodels.LogMessage, error) {
	logMsgs := []apimodels.LogMessage{}
	numMsgsNeeded := numMsgs
	lastTimeStamp := time.Now().Add(24 * time.Hour)

	oldMsgTypes := []string{}
	for _, msgType := range msgTypes {
		switch msgType {
		case apimodels.SystemLogPrefix:
			oldMsgTypes = append(oldMsgTypes, "system")
		case apimodels.AgentLogPrefix:
			oldMsgTypes = append(oldMsgTypes, "agent")
		case apimodels.TaskLogPrefix:
			oldMsgTypes = append(oldMsgTypes, "task")
		}
	}

	// keep grabbing task logs from farther back until there are enough messages
	for numMsgsNeeded != 0 {
		numTaskLogsToFetch := numMsgsNeeded / MessagesPerLog
		taskLogs, err := FindTaskLogsBeforeTime(taskId, execution, lastTimeStamp,
			numTaskLogsToFetch)
		if err != nil {
			return nil, err
		}
		// if we've exhausted the stored logs, break
		if len(taskLogs) == 0 {
			break
		}

		// otherwise, grab all applicable log messages out of the returned task
		// log documents
		for _, taskLog := range taskLogs {
			// reverse
			messages := make([]apimodels.LogMessage, len(taskLog.Messages))
			for idx, msg := range taskLog.Messages {
				messages[len(taskLog.Messages)-1-idx] = msg
			}
			for _, logMsg := range messages {
				// filter by severity and type
				if len(severities) != 0 &&
					!utility.StringSliceContains(severities, logMsg.Severity) {
					continue
				}
				if len(msgTypes) != 0 {
					if !(utility.StringSliceContains(msgTypes, logMsg.Type) ||
						utility.StringSliceContains(oldMsgTypes, logMsg.Type)) {
						continue
					}
				}
				// the message is relevant, store it
				logMsgs = append(logMsgs, logMsg)
				numMsgsNeeded--
				if numMsgsNeeded == 0 {
					return logMsgs, nil
				}
			}
		}
		// store the last timestamp
		lastTimeStamp = taskLogs[len(taskLogs)-1].Timestamp
	}

	return logMsgs, nil
}
