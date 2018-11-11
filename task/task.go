// Package task defines the task meta data
package task

import (
	"overlord/proto"
)

// OpType is the operation of task name
type OpType = string

// define Optration types
const (
	// create means create empty cluster into metadata and scale nodes into given.
	OpCreate OpType = "create"

	// OpDestroy means that destroy the whole cluster.
	OpDestroy OpType = "destroy"

	// scale may be scale with given node count
	OpScale OpType = "scale"

	// OpStretch will scale the instance memory and may migrating slot.
	OpStretch OpType = "stretch"

	// OpDel means deal nodes from given cluster: with random given node or special name(id)
	OpDel OpType = "delete"

	// OpFix will trying to run `rustkit fix` to the given cluster(redis cluster only)
	OpFix OpType = "fix"

	// Balance will balance the given cluster
	OpBalance OpType = "balance"
)

// Task is a single POD type which represent a single task.
type Task struct {
	// Order was generated by etcd post
	ID        string
	Name      string
	CacheType proto.CacheType
	Version   string  // service version
	Num       int     // num of instances ,if redis-cluster,mean master number.
	MaxMem    float64 // max memory use of instance.
	CPU       float64 // cpu count for each instance.
	I         *Instance
	// Scheduler is the name of scheduler and path of etcd
	Scheduler string

	OpType OpType

	// Users to apply that
	// the first is the task commiter
	Users []string

	// cluster must never be absent unless create.
	Cluster string `json:"omitempty"`

	// Params is the given parameters by the frontend interface.
	Params map[string]string

	// ParamsValid is the function which check need to check the tasks.
	ParamsValid func(*Task, map[string]string) (bool, map[string]string) `json:"-"`

	// Args is the auto gennerated arguments for the whole task
	// maybe:
	//     role map
	//     nodes
	//     template data
	//     and so on.
	Args map[string]interface{}

	// ArgsValid is the function which check need to check the tasks.
	ArgsValid func(*Task, map[string]interface{}) (bool, map[string]string) `json:"-"`
}

// Instance  detail.
type Instance struct {
	Name   string
	Memory float64 // capacity of memory of the instance in MB
	CPU    float64 // num of cpu cors if the instance
}
