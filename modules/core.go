// Package modules contains the module system used in the hotstuff project.
// The module system allows us to use different implementations of key components,
// such as the crypto module or the consensus module,
// and ensures that each module has access to the other modules it depends on.
//
// There are two main reason one might want to use the module system for a component:
//
// 1. To give the component access to other modules.
//
// 2. To give other modules access to the component.
//
// To be able to access other modules from a struct, you will need to implement the Module interface from this package.
// The InitModule method of the Module interface gives your struct a pointer to the Core object, which can be used
// to obtain pointers to the other modules.
// If your module will be interacting with the event loop,
// then this method is the preferred location to set up handlers for events.
//
// Finally, to set up the module system and its modules, you must create a Builder using the NewBuilder function,
// and then all your modules to the builder using the Add method. For example:
//
//	builder := NewBuilder()
//	// replace the logger
//	builder.Add(logging.New("foo"))
//	mods := builder.Build()
//
// If two modules satisfy the same interface, then the one that was registered last will be returned by the module system,
// though note that both modules will be initialized if they implement the Module interface.
//
// After building the module system, you can use the TryGet or Get methods to get pointers to the modules:
//
//	var module MyModule
//	mods.Get(&module)
package modules

import (
	"fmt"
	"reflect"

	"github.com/relab/hotstuff"
)

type PipelineId uint32

// type ModuleTypeId uint32

type Pipeline []Module

const PipelineIdNone = ^PipelineId(0)

// Module is an interface for initializing modules.
type Module interface {
	InitModule(mods *Core)
}

// Core is the base of the module system.
// It contains only a few core modules that are shared between replicas and clients.
type Core struct {
	staticModules    []any
	pipelinedModules map[PipelineId]Pipeline
}

// TryGet attempts to find a module for ptr.
// TryGet returns true if a module was stored in ptr, false otherwise.
//
// NOTE: ptr must be a non-nil pointer to a type that has been provided to the module system.
//
// Example:
//
//	builder := modules.New()
//	builder.Add(MyModuleImpl{})
//	mods = builder.Build()
//
//	var module MyModule
//	if mods.TryGet(&module) {
//		// success
//	}
func (mods Core) TryGet(ptr any) bool {
	v := reflect.ValueOf(ptr)
	if !v.IsValid() {
		panic("nil value given")
	}
	pt := v.Type()
	if pt.Kind() != reflect.Ptr {
		panic("only pointer values allowed")
	}

	for _, m := range mods.staticModules {
		mv := reflect.ValueOf(m)
		if mv.Type().AssignableTo(pt.Elem()) {
			v.Elem().Set(mv)
			return true
		}
	}

	return false
}

// Get finds compatible modules for the given pointers.
//
// NOTE: pointers must only contain non-nil pointers to types that have been provided to the module system.
// Get panics if one of the given arguments is not a pointer, or if a compatible module is not found.
//
// Example:
//
//	builder := modules.New()
//	builder.Add(MyModuleImpl{})
//	mods = builder.Build()
//
//	var module MyModule
//	mods.Get(&module)
func (mods *Core) Get(pointers ...any) {
	if len(pointers) == 0 {
		panic("no pointers given")
	}
	for _, ptr := range pointers {
		if !mods.TryGet(ptr) {
			panic(fmt.Sprintf("module of type %s not found", reflect.TypeOf(ptr).Elem()))
		}
	}
}

// Assign a module reference
func (mods *Core) TryAssignPipelined(self Module, ptr any) bool {
	v := reflect.ValueOf(ptr)
	// if !v.IsValid() {
	// 	panic("nil value given")
	// }
	pt := v.Type()
	if pt.Kind() != reflect.Ptr {
		panic("only pointer values allowed")
	}

	// TODO: Handle the case of pipelineCount = 0

	correctPipelineId := PipelineIdNone
	for id := range mods.pipelinedModules {
		pipeline := mods.pipelinedModules[id]
		// Check if self is in pipeline
		for _, module := range pipeline {
			if module == self {
				correctPipelineId = id
				break
			}
		}
		// Break outer loop too if a pipeline ID was found
		if correctPipelineId != PipelineIdNone {
			break
		}
	}

	// If this variable remained unchanged, return false
	if correctPipelineId == PipelineIdNone {
		return false
	}

	correctPipeline := mods.pipelinedModules[correctPipelineId]
	for _, m := range correctPipeline {
		mv := reflect.ValueOf(m)
		if mv.Type().AssignableTo(pt.Elem()) {
			v.Elem().Set(mv)
			return true
		}
	}

	return false
}

// func (mods *Core) FindModuleFromPipeline(typeId ModuleTypeId, pipelineId PipelineId) (any, bool) {
// 	module, ok := mods.pipelinedModules[pipelineId][typeId]
// 	return module, ok
// }

// func (mods *Core) FindModulePipelineId(ptr any) PipelineId {
// 	for id := range mods.pipelinedModules {
// 		pipeline := mods.pipelinedModules[id]
// 		for typeId := range pipeline {
// 			modRef := pipeline[typeId]
// 			if modRef == ptr {
// 				return id
// 			}
// 		}
// 	}
//
// 	return PipelineIdNone
// }

// func (mods *Core) GetAllPipelinedOfType(t ModuleTypeId) []Module {
// 	m := make([]Module, 0)
// 	for _, pipeline := range mods.pipelinedModules {
// 		m = append(m, pipeline[t].(Module))
// 	}
// 	return m
// }

// Builder is a helper for setting up client modules.
type Builder struct {
	core            Core
	staticModules   []Module
	modulePipelines map[PipelineId]Pipeline
	opts            *Options
	pipelineCount   int
}

// NewBuilder returns a new builder.
// Alan: Builder now requires a pipeline count. If zero is specified, pipelined modules will be
// insterted to staticModules in further function calls.
func NewBuilder(id hotstuff.ID, pk hotstuff.PrivateKey, pipelineCount uint) Builder {
	bl := Builder{
		opts: &Options{
			id:                 id,
			privateKey:         pk,
			connectionMetadata: make(map[string]string),
		},
	}

	if pipelineCount == 0 {
		bl.pipelineCount = 0
		bl.modulePipelines = nil
		return bl // Early exit
	}

	bl.pipelineCount = int(pipelineCount)
	bl.modulePipelines = make(map[PipelineId]Pipeline)
	for i := uint(0); i < pipelineCount; i++ {
		id := PipelineId(i)
		bl.modulePipelines[id] = make(Pipeline, 0)
	}

	return bl
}

// Options returns the options module.
func (b *Builder) Options() *Options {
	return b.opts
}

// AddStatic adds existing, singular, module instances to the builder.
func (b *Builder) AddStatic(modules ...any) {
	b.core.staticModules = append(b.core.staticModules, modules...)
	for _, module := range modules {
		if m, ok := module.(Module); ok {
			b.staticModules = append(b.staticModules, m)
		}
	}
}

// AddPipelined constructs and adds several instances of a module based on b.pipelineCount,
// provided its constructor arguments. If b.pipelineCount == 0 then one will be constructed and added
// as a static module.
func (b *Builder) AddPipelined(ctor any, ctorArgs ...any) {
	if reflect.TypeOf(ctor).Kind() != reflect.Func {
		panic("second argument is not a function")
	}

	vargs := make([]reflect.Value, len(ctorArgs))
	for n, v := range ctorArgs {
		vargs[n] = reflect.ValueOf(v)
	}

	ctorVal := reflect.ValueOf(ctor)
	if b.pipelineCount == 0 {
		returnResult := ctorVal.Call(vargs)
		if len(returnResult) != 1 {
			panic("constructor does not return a single value")
		}
		mod := returnResult[0].Interface()
		b.AddStatic(mod)
		return
	}

	for id := range b.modulePipelines {
		returnResult := ctorVal.Call(vargs)
		if len(returnResult) != 1 {
			panic("constructor does not return a single value")
		}
		mod := returnResult[0].Interface()
		converted, ok := mod.(Module)
		if !ok {
			panic("constructor did not construct a value of type Module")
		}
		b.modulePipelines[id] = append(b.modulePipelines[id], converted)
	}
}

func (b *Builder) PipelineCount() int {
	return len(b.modulePipelines)
}

func (b *Builder) GetPipeline(id PipelineId) Pipeline {
	return b.modulePipelines[id]
}

// Build initializes all added modules and returns the Core object.
func (b *Builder) Build() *Core {
	// reverse the order of the added modules so that TryGet will find the latest first.
	for i, j := 0, len(b.core.staticModules)-1; i < j; i, j = i+1, j-1 {
		b.core.staticModules[i], b.core.staticModules[j] = b.core.staticModules[j], b.core.staticModules[i]
	}
	// add the Options last so that it can be overridden by user.
	b.AddStatic(b.opts)
	for _, module := range b.staticModules {
		module.InitModule(&b.core)
	}

	// Adding the pipelined modules to core.
	b.core.pipelinedModules = make(map[PipelineId]Pipeline)
	for id, pipeline := range b.modulePipelines {
		b.core.pipelinedModules[id] = make(Pipeline, 0)
		for _, module := range pipeline {
			b.core.pipelinedModules[id] = append(b.core.pipelinedModules[id], module)
		}
	}

	// Initializing later so that modules can reference
	// other modules in the same pipeline without panicking.
	for _, pipeline := range b.core.pipelinedModules {
		for _, module := range pipeline {
			module.(Module).InitModule(&b.core)
		}
	}
	return &b.core
}
