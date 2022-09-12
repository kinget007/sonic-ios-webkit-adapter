package protocols

import (
	"encoding/json"
	"fmt"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"log"
	"regexp"
	"sonic-ios-webkit-adapter/adapter"
	"sonic-ios-webkit-adapter/entity"
	"strconv"
	"strings"
	"time"
)

func NewProtocolAdapter(adapter *adapters.Adapter, version string) *ProtocolAdapter {
	protocol := &ProtocolAdapter{
		adapter: adapter,
	}
	parts := strings.Split(version, ".")
	if len(parts) > 0 {
		major, err := strconv.Atoi(parts[0])
		if err != nil {
			log.Panic(err)
		}
		if major <= 8 {
			initIOS8(protocol)
			return protocol
		}
		minor, err := strconv.Atoi(parts[1])
		if err != nil {
			log.Panic(err)
		}
		if major > 12 || major >= 12 && minor >= 2 {
			initIOS12(protocol)
			return protocol
		}
	}
	initIOS9(protocol)
	return protocol
}

type mapSelectorListFunc func(selectorList gjson.Result, message string) string

type ProtocolAdapter struct {
	adapter                    *adapters.Adapter
	lastNodeId                 int64
	lastPageExecutionContextId int64
	styleMap                   map[string]interface{}
	lastScriptEval             interface{}
	screencast                 *screencastSession
	mapSelectorList            mapSelectorListFunc
}

func (p *ProtocolAdapter) init() {
	p.styleMap = make(map[string]interface{})

	p.adapter.AddMessageFilter("DOM.getDocument", p.onDomGetDocument)
	// CSS
	p.adapter.AddMessageFilter("CSS.setStyleTexts", p.onSetStyleTexts)
	p.adapter.AddMessageFilter("CSS.getMatchedStylesForNode", p.onGetMatchedStylesForNode)
	p.adapter.AddMessageFilter("CSS.getBackgroundColors", p.onGetBackgroundColors)
	p.adapter.AddMessageFilter("CSS.addRule", p.onAddRule)
	p.adapter.AddMessageFilter("CSS.getPlatformFontsForNode", p.onGetPlatformFontsForNode)

	p.adapter.AddMessageFilter("CSS.getMatchedStylesForNode", p.onGetMatchedStylesForNodeResult)
	// Page
	p.adapter.AddMessageFilter("Page.startScreencast", p.onStartScreencast)
	p.adapter.AddMessageFilter("Page.stopScreencast", p.onStopScreencast)
	p.adapter.AddMessageFilter("Page.screencastFrameAck", p.onScreencastFrameAck)
	p.adapter.AddMessageFilter("Page.getNavigationHistory", p.onGetNavigationHistory)
	p.adapter.AddMessageFilter("Page.setOverlayMessage", p.onPageSetOverlay)
	p.adapter.AddMessageFilter("Page.configureOverlay", p.onPageConfigureOverlay)
	// DOM
	p.adapter.AddMessageFilter("DOM.enable", p.onDomEnable)
	p.adapter.AddMessageFilter("DOM.setInspectMode", p.onSetInspectMode)
	p.adapter.AddMessageFilter("DOM.setInspectedNode", p.onDomSetInspectedNode)
	p.adapter.AddMessageFilter("DOM.pushNodesByBackendIdsToFrontend", p.onPushNodesByBackendIdsToFrontend)
	p.adapter.AddMessageFilter("DOM.getBoxModel", p.onGetBoxModel)
	p.adapter.AddMessageFilter("DOM.getNodeForLocation", p.onGetNodeForLocation)
	// DOMDebugger
	p.adapter.AddMessageFilter("DOMDebugger.getEventListeners", p.domDebuggerOnGetEventListeners)
	// Debugger
	p.adapter.AddMessageFilter("Debugger.canSetScriptSource", p.onCanSetScriptSource)
	p.adapter.AddMessageFilter("Debugger.setBlackboxPatterns", p.onSetBlackboxPatterns)
	p.adapter.AddMessageFilter("Debugger.setAsyncCallStackDepth", p.onSetAsyncCallStackDepth)
	p.adapter.AddMessageFilter("Debugger.enable", p.onDebuggerEnable)

	p.adapter.AddMessageFilter("Debugger.scriptParsed", p.onScriptParsed)
	// Emulation
	p.adapter.AddMessageFilter("Emulation.canEmulate", p.onCanEmulate)
	p.adapter.AddMessageFilter("Emulation.setTouchEmulationEnabled", p.onEmulationSetTouchEmulationEnabled)
	p.adapter.AddMessageFilter("Emulation.setScriptExecutionDisabled", p.onEmulationSetScriptExecutionDisabled)
	p.adapter.AddMessageFilter("Emulation.setEmulatedMedia", p.onEmulationSetEmulatedMedia)
	// Rendering
	p.adapter.AddMessageFilter("Rendering.setShowPaintRects", p.onRenderingSetShowPaintRects)
	// Input
	p.adapter.AddMessageFilter("Input.emulateTouchFromMouseEvent", p.onEmulateTouchFromMouseEvent)
	// Log
	p.adapter.AddMessageFilter("Log.clear", p.onLogClear)
	p.adapter.AddMessageFilter("Log.disable", p.onLogDisable)
	p.adapter.AddMessageFilter("Log.enable", p.onLogEnable)
	// Console
	p.adapter.AddMessageFilter("Console.messageAdded", p.onConsoleMessageAdded)
	// Network
	p.adapter.AddMessageFilter("Network.getCookies", p.onNetworkGetCookies)
	p.adapter.AddMessageFilter("Network.deleteCookie", p.onNetworkDeleteCookie)
	p.adapter.AddMessageFilter("Network.setMonitoringXHREnabled", p.onNetworkSetMonitoringXHREnabled)
	p.adapter.AddMessageFilter("Network.canEmulateNetworkConditions", p.onCanEmulateNetworkConditions)
	// Runtime
	p.adapter.AddMessageFilter("Runtime.compileScript", p.onRuntimeOnCompileScript)
	p.adapter.AddMessageFilter("Runtime.executionContextCreated", p.onExecutionContextCreated)
	p.adapter.AddMessageFilter("Runtime.evaluate", p.onEvaluate)
	p.adapter.AddMessageFilter("Runtime.getProperties", p.onRuntimeGetProperties)
	// Inspector
	p.adapter.AddMessageFilter("Inspector.inspect", p.onInspect)
}

func (p *ProtocolAdapter) defaultCallFunc(message []byte) {
	//log.Println(string(message))
}

func (p *ProtocolAdapter) onDomGetDocument(message []byte) []byte {
	p.enumerateStyleSheets(message)
	return message
}

func (p *ProtocolAdapter) onPageSetOverlay(message []byte) []byte {
	method := "Debugger.setOverlayMessage"
	return ReplaceMethodNameAndOutputBinary(message, method)
}

func (p *ProtocolAdapter) onPageConfigureOverlay(message []byte) []byte {
	return p.onPageSetOverlay(message)
}

func (p *ProtocolAdapter) onDomSetInspectedNode(message []byte) []byte {
	method := "Console.addInspectedNode"
	return ReplaceMethodNameAndOutputBinary(message, method)
}

func (p *ProtocolAdapter) onEmulationSetTouchEmulationEnabled(message []byte) []byte {
	method := "Page.setTouchEmulationEnabled"
	return ReplaceMethodNameAndOutputBinary(message, method)
}

func (p *ProtocolAdapter) onEmulationSetScriptExecutionDisabled(message []byte) []byte {
	method := "Page.setScriptExecutionDisabled"
	return ReplaceMethodNameAndOutputBinary(message, method)
}

func (p *ProtocolAdapter) onEmulationSetEmulatedMedia(message []byte) []byte {
	method := "Page.setEmulatedMedia"
	return ReplaceMethodNameAndOutputBinary(message, method)
}

func (p *ProtocolAdapter) onRenderingSetShowPaintRects(message []byte) []byte {
	method := "Page.setShowPaintRects"
	return ReplaceMethodNameAndOutputBinary(message, method)
}

func (p *ProtocolAdapter) onLogClear(message []byte) []byte {
	method := "Console.clearMessages"
	return ReplaceMethodNameAndOutputBinary(message, method)
}

func (p *ProtocolAdapter) onLogDisable(message []byte) []byte {
	method := "Console.disable"
	return ReplaceMethodNameAndOutputBinary(message, method)
}

func (p *ProtocolAdapter) onLogEnable(message []byte) []byte {
	method := "Console.enable"
	return ReplaceMethodNameAndOutputBinary(message, method)
}
func (p *ProtocolAdapter) onNetworkGetCookies(message []byte) []byte {
	method := "Page.getCookies"
	return ReplaceMethodNameAndOutputBinary(message, method)
}

func (p *ProtocolAdapter) onNetworkDeleteCookie(message []byte) []byte {
	method := "Page.deleteCookie"
	return ReplaceMethodNameAndOutputBinary(message, method)
}

func (p *ProtocolAdapter) onNetworkSetMonitoringXHREnabled(message []byte) []byte {
	method := "Console.setMonitoringXHREnabled"
	return ReplaceMethodNameAndOutputBinary(message, method)
}

func (p *ProtocolAdapter) onGetMatchedStylesForNode(message []byte) []byte {
	p.lastNodeId = gjson.Get(string(message), "params.nodeId").Int()
	return message
}

func (p *ProtocolAdapter) onCanEmulate(message []byte) []byte {
	result := map[string]interface{}{
		"result": true,
	}
	p.adapter.FireResultToTools(int(gjson.Get(string(message), "id").Int()), result)
	return nil
}

func (p *ProtocolAdapter) onGetPlatformFontsForNode(message []byte) []byte {
	result := map[string]interface{}{
		"fonts": []string{},
	}
	p.adapter.FireResultToTools(int(gjson.Get(string(message), "id").Int()), result)
	return nil
}

func (p *ProtocolAdapter) onGetBackgroundColors(message []byte) []byte {
	result := map[string]interface{}{
		"backgroundColors": []string{},
	}
	p.adapter.FireResultToTools(int(gjson.Get(string(message), "id").Int()), result)
	return nil
}

func (p *ProtocolAdapter) onCanSetScriptSource(message []byte) []byte {
	result := map[string]interface{}{
		"result": false,
	}
	p.adapter.FireResultToTools(int(gjson.Get(string(message), "id").Int()), result)
	return nil
}

func (p *ProtocolAdapter) onSetBlackboxPatterns(message []byte) []byte {
	result := map[string]interface{}{}
	p.adapter.FireResultToTools(int(gjson.Get(string(message), "id").Int()), result)
	return nil
}

func (p *ProtocolAdapter) onSetAsyncCallStackDepth(message []byte) []byte {
	result := map[string]interface{}{
		"result": true,
	}
	p.adapter.FireResultToTools(int(gjson.Get(string(message), "id").Int()), result)
	return nil
}

func (p *ProtocolAdapter) onDebuggerEnable(message []byte) []byte {
	p.adapter.CallTarget("Debugger.setBreakpointsActive", map[string]interface{}{
		"active": true,
	}, p.defaultCallFunc)
	return message
}

func (p *ProtocolAdapter) onExecutionContextCreated(message []byte) []byte {
	msg := string(message)
	var err error
	if gjson.Get(msg, "params").Exists() && gjson.Get(msg, "params.context").Exists() {
		if !gjson.Get(msg, "params.context.origin").Exists() {
			msg, err = sjson.Set(msg, "params.context.origin", gjson.Get(msg, "params.context.name").String())
			if err != nil {
				log.Panic(err)
			}
			if gjson.Get(msg, "params.context.isPageContext").Exists() {
				p.lastPageExecutionContextId = gjson.Get(msg, "params.context.id").Int()
			}
			if gjson.Get(msg, "params.context.frameId").Exists() {
				msg, err = sjson.Set(msg, "params.context.auxData", map[string]interface{}{
					"frameId":   gjson.Get(msg, "params.context.frameId").String(),
					"isDefault": true,
				})
				if err != nil {
					log.Panic(err)
				}
				msg, err = sjson.Delete(msg, "params.context.frameId")
				if err != nil {
					log.Panic(err)
				}
			}
		}
	}

	return []byte(msg)
}

func (p *ProtocolAdapter) onEvaluate(message []byte) []byte {
	msg := string(message)
	var err error
	result := gjson.Get(msg, "result")
	if result.Exists() && result.Get("wasThrown").Exists() {
		msg, err = sjson.Set(msg, "result.result.subtype", "error")
		if err != nil {
			return nil
		}
		arr, err1 := json.Marshal(map[string]interface{}{
			"text":     gjson.Get(msg, "result.result.description").Value(),
			"url":      "",
			"scriptId": p.lastScriptEval,
			"line":     1,
			"column":   0,
			"stack": map[string]interface{}{
				"callFrames": []map[string]interface{}{
					{
						"functionName": "",
						"scriptId":     p.lastScriptEval,
						"url":          "",
						"lineNumber":   1,
						"columnNumber": 1,
					},
				},
			},
		})
		if err1 != nil {
			log.Panic(err)
		}
		msg, err = sjson.Set(msg, "result.exceptionDetails", string(arr))
		if err != nil {
			log.Panic(err)
		}
	} else if result.Exists() && result.Get("result").Exists() && result.Get("result.preview").Exists() {
		msg, err = sjson.Set(msg, "result.result.preview.description", gjson.Get(msg, "result.result.description").Value())
		if err != nil {
			log.Panic(err)
		}
		msg, err = sjson.Set(msg, "result.result.preview.type", "object")
		if err != nil {
			log.Panic(err)
		}
	}
	return []byte(msg)
}

func (p *ProtocolAdapter) onRuntimeOnCompileScript(message []byte) []byte {
	params := map[string]interface{}{
		"expression": gjson.Get(string(message), "params.expression").String(),
		"contextId":  gjson.Get(string(message), "params.executionContextId").Int(),
	}
	p.adapter.CallTarget("Runtime.evaluate", params, func(msg []byte) {
		result := map[string]interface{}{
			"scriptId":         nil,
			"exceptionDetails": nil,
		}
		p.adapter.FireResultToTools(int(gjson.Get(string(message), "id").Int()), result)
	})
	return nil
}

func (p *ProtocolAdapter) onRuntimeGetProperties(message []byte) []byte {
	var newPropertyDescriptors []interface{}
	var err error
	msg := string(message)
	for _, node := range gjson.Get(msg, "result.result").Array() {
		isOwn := node.Get("isOwn")
		nativeGetter := node.Get("nativeGetter")
		if isOwn.Exists() || nativeGetter.Exists() {
			msg, err = sjson.Set(msg, isOwn.Path(string(message)), true)
			if err != nil {
				log.Panic(err)
			}
			newPropertyDescriptors = append(newPropertyDescriptors, gjson.Get(msg, node.Path(msg)).Value())
		}
	}
	msg, err = sjson.Set(msg, "result.result", newPropertyDescriptors)
	if err != nil {
		log.Panic(err)
	}
	return []byte(msg)
}

func (p *ProtocolAdapter) onScriptParsed(message []byte) []byte {
	p.lastScriptEval = gjson.Get(string(message), "params.scriptId")
	return message
}

func (p *ProtocolAdapter) onDomEnable(message []byte) []byte {
	p.adapter.FireResultToTools(int(gjson.Get(string(message), "id").Int()), map[string]interface{}{})
	return nil
}

func (p *ProtocolAdapter) onSetInspectMode(message []byte) []byte {
	msg := string(message)
	var err error
	msg, err = sjson.Set(msg, "method", "DOM.setInspectModeEnabled")
	if err != nil {
		log.Panic(err)
	}
	msg, err = sjson.Set(msg, "params.enabled",
		gjson.Get(msg, "params.mode").String() == "searchForNode")
	if err != nil {
		log.Panic(err)
	}
	msg, err = sjson.Delete(msg, "params.mode")
	if err != nil {
		log.Panic(err)
	}
	return []byte(msg)
}

func (p *ProtocolAdapter) onInspect(message []byte) []byte {
	msg := string(message)
	var err error
	msg, err = sjson.Set(msg, "method", "DOM.inspectNodeRequested")
	if err != nil {
		log.Panic(err)
	}
	msg, err = sjson.Set(msg, "params.backendNodeId", gjson.Get(msg, "params.object.objectId").Value())
	if err != nil {
		log.Panic(err)
	}
	msg, err = sjson.Delete(msg, "params.object")
	if err != nil {
		log.Panic(err)
	}
	msg, err = sjson.Delete(msg, "params.hints")
	if err != nil {
		log.Panic(err)
	}
	return []byte(msg)
}

func (p *ProtocolAdapter) domDebuggerOnGetEventListeners(message []byte) []byte {
	requestNodeParams := map[string]interface{}{
		"objectId": gjson.Get(string(message), "params.objectId").Value(),
	}
	p.adapter.CallTarget("DOM.requestNode", requestNodeParams, func(result []byte) {
		getEventListenersForNodeParams := map[string]interface{}{
			"nodeId":      gjson.Get(string(result), "result.nodeId").Value(),
			"objectGroup": "event-listeners-panel",
		}
		p.adapter.CallTarget("DOM.getEventListenersForNode", getEventListenersForNodeParams, func(msg []byte) {
			listeners := gjson.Get(string(msg), "listeners").Array()
			var mappedListeners []map[string]interface{}
			for _, listener := range listeners {
				mappedListeners = append(mappedListeners, map[string]interface{}{
					"type":       listener.Get("type").Value(),
					"useCapture": listener.Get("useCapture").Value(),
					"passive":    false, // iOS doesn't support this property, http://compatibility.remotedebug.org/DOM/Safari%20iOS%209.3/types/EventListener,
					"location":   listener.Get("location").Value(),
					"hander":     listener.Get("hander").Value(),
				})
			}
			mappedResult := map[string]interface{}{
				"listeners": mappedListeners,
			}
			p.adapter.FireResultToTools(int(gjson.Get(string(message), "id").Int()), mappedResult)
		})
	})
	return nil
}

func (p *ProtocolAdapter) onPushNodesByBackendIdsToFrontend(message []byte) []byte {
	id := gjson.Get(string(message), "id").Int()
	var resultBackNodeIds []interface{}
	for _, backNode := range gjson.Get(string(message), "params.backendNodeIds").Array() {
		params := map[string]interface{}{
			"backendNodeId": backNode.Value(),
		}
		p.adapter.CallTarget("DOM.pushNodeByBackendIdToFrontend", params, func(msg []byte) {
			resultBackNodeIds = append(resultBackNodeIds, gjson.Get(string(msg), "nodeId").Value())
		})
	}
	result := map[string]interface{}{
		"nodeIds": resultBackNodeIds,
	}
	p.adapter.FireResultToTools(int(id), result)
	return nil
}

func (p *ProtocolAdapter) onGetBoxModel(message []byte) []byte {
	params := map[string]interface{}{
		"highlightConfig": map[string]interface{}{
			"showInfo":           true,
			"showRulers":         false,
			"showExtensionLines": false,
			"contentColor":       map[string]interface{}{"r": 111, "g": 168, "b": 220, "a": 0.66},
			"paddingColor":       map[string]interface{}{"r": 147, "g": 196, "b": 125, "a": 0.55},
			"borderColor":        map[string]interface{}{"r": 255, "g": 229, "b": 153, "a": 0.66},
			"marginColor":        map[string]interface{}{"r": 246, "g": 178, "b": 107, "a": 0.66},
			"eventTargetColor":   map[string]interface{}{"r": 255, "g": 196, "b": 196, "a": 0.66},
			"shapeColor":         map[string]interface{}{"r": 96, "g": 82, "b": 177, "a": 0.8},
			"shapeMarginColor":   map[string]interface{}{"r": 96, "g": 82, "b": 127, "a": 0.6},
			"displayAsMaterial":  true,
		},
		"nodeId": gjson.Get(string(message), "params.nodeId").Value(),
	}
	p.adapter.CallTarget("DOM.highlightNode", params, func(message []byte) {

	})
	return nil
}

func (p *ProtocolAdapter) onGetNodeForLocation(message []byte) []byte {
	evaluateParams := map[string]interface{}{
		"expression": fmt.Sprintf("document.elementFromPoint(%d,%d)", gjson.Get(string(message), "params.x").Int(), gjson.Get(string(message), "params.y").Int()),
	}
	p.adapter.CallTarget("Runtime.evaluate", evaluateParams, func(result []byte) {
		requestNodeParams := map[string]interface{}{
			"objectId": gjson.Get(string(result), "result.objectId").Value(),
		}
		p.adapter.CallTarget("DOM.requestNode", requestNodeParams, func(msg []byte) {
			resultParams := map[string]interface{}{
				"nodeId": gjson.Get(string(msg), "nodeId").Value(),
			}
			p.adapter.FireResultToTools(int(gjson.Get(string(message), "id").Int()), resultParams)
		})
	})
	return nil
}

// todo screencast
func (p *ProtocolAdapter) onStartScreencast(message []byte) []byte {
	params := gjson.Get(string(message), "params")
	format := params.Get("format").String()
	quality := params.Get("quality").Int()
	maxWidth := params.Get("maxWidth").Int()
	maxHeight := params.Get("maxHeight").Int()
	if p.screencast != nil {
		// clear previous session
		p.screencast.stop()
	}
	p.screencast = newScreencastSession(p.adapter,
		WithFormat(format),
		WithMaxWidth(int(maxWidth)),
		WithMaxHeight(int(maxHeight)),
		WithQuality(int(quality)))
	p.screencast.start()

	p.adapter.FireResultToTools(int(gjson.Get(string(message), "id").Int()), map[string]interface{}{})

	return nil
}

func (p *ProtocolAdapter) onStopScreencast(message []byte) []byte {
	if p.screencast != nil {
		// clear previous session
		p.screencast.stop()
		p.screencast = nil
	}
	p.adapter.FireResultToTools(int(gjson.Get(string(message), "id").Int()), map[string]interface{}{})

	return nil
}

func (p *ProtocolAdapter) onScreencastFrameAck(message []byte) []byte {
	if p.screencast != nil {
		frameNumber := gjson.Get(string(message), "params.sessionId").Int()
		// todo Change to int 64?
		p.screencast.ackFrame(int(frameNumber))
	}
	p.adapter.FireResultToTools(int(gjson.Get(string(message), "id").Int()), map[string]interface{}{})

	return nil
}

func (p *ProtocolAdapter) onGetNavigationHistory(message []byte) []byte {
	var href string
	var id = int(gjson.Get(string(message), "id").Int())
	p.adapter.CallTarget("Runtime.evaluate", map[string]interface{}{"expression": "window.location.href"}, func(result []byte) {
		href = gjson.Get(string(result), "result.value").String()
		p.adapter.CallTarget("Runtime.evaluate", map[string]interface{}{"expression": "window.title"}, func(msg []byte) {
			title := gjson.Get(string(msg), "result.value").String()
			p.adapter.FireResultToTools(id, map[string]interface{}{
				"currentIndex": 0, "entries": []interface{}{
					map[string]interface{}{
						"id":    0,
						"url":   href,
						"title": title,
					},
				},
			})
		})
	})
	return nil
}

func (p *ProtocolAdapter) onEmulateTouchFromMouseEvent(message []byte) []byte {
	var funcStr = `function simulate(params) {
                const element = document.elementFromPoint(params.x, params.y);
                const e = new MouseEvent(params.type, {
                    screenX: params.x,
                    screenY: params.y,
                    clientX: 0,
                    clientY: 0,
                    ctrlKey: (params.modifiers & 2) === 2,
                    shiftKey: (params.modifiers & 8) === 8,
                    altKey: (params.modifiers & 1) === 1,
                    metaKey: (params.modifiers & 4) === 4,
                    button: params.button,
                    bubbles: true,
                    cancelable: false
                });
                element.dispatchEvent(e);
                return element;
            }`
	msg := string(message)
	var err error
	switch gjson.Get(msg, "params.type").String() {
	case "mousePressed":
		msg, err = sjson.Set(msg, "params.type", "mousedown")
		if err != nil {
			log.Panic(err)
		}
		break
	case "mouseReleased":
		msg, err = sjson.Set(msg, "params.type", "click")
		if err != nil {
			log.Panic(err)
		}
		break
	case "mouseMoved":
		msg, err = sjson.Set(msg, "params.type", "mousemove")
		if err != nil {
			log.Panic(err)
		}
		break
	default:
		log.Panic(fmt.Sprintf("Unknown emulate mouse event name %s", gjson.Get(msg, "params.type").String()))
	}
	var exp = fmt.Sprintf("(%s)(%s)", funcStr, msg)

	p.adapter.CallTarget("Runtime.evaluate", map[string]interface{}{
		"expression": exp,
	}, func(result []byte) {
		if gjson.Get(msg, "params.type").String() == "click" {
			msg, err = sjson.Set(msg, "params.type", "mouseup")
			if err != nil {
				log.Panic(err)
			}
		}
		p.adapter.CallTarget("Runtime.evaluate", map[string]interface{}{
			"expression": exp,
		}, func(message []byte) {

		})
	})
	return p.adapter.ReplyWithEmpty(msg)
}

func (p *ProtocolAdapter) onCanEmulateNetworkConditions(message []byte) []byte {
	result := map[string]interface{}{
		"result": false,
	}
	p.adapter.FireResultToTools(int(gjson.Get(string(message), "id").Int()), result)
	return nil
}

func (p *ProtocolAdapter) onConsoleMessageAdded(message []byte) []byte {
	resultMessage := gjson.Get(string(message), "params.message")
	messageType := resultMessage.Get("type").String()
	var resultType string
	if resultType == "log" {
		switch messageType {
		case "log":
			resultType = "log"
		case "info":
			resultType = "info"
		case "error":
			resultType = "error"
		default:
			resultType = "log"
		}
	} else {
		resultType = "log"
	}
	consoleMessage := map[string]interface{}{
		"source":           gjson.Get(string(message), "source").Value(),
		"level":            resultType,
		"text":             gjson.Get(string(message), "text").Value(),
		"lineNumber":       gjson.Get(string(message), "line").Value(),
		"timestamp":        time.Now().UnixNano(),
		"url":              gjson.Get(string(message), "url").Value(),
		"networkRequestId": gjson.Get(string(message), "networkRequestId").Value(),
	}
	if gjson.Get(string(message), "stackTrace").Exists() {
		consoleMessage["stackTrace"] = map[string]interface{}{
			"callFrames": gjson.Get(string(message), "stackTrace").Value(),
		}
	} else {
		consoleMessage["stackTrace"] = nil
	}
	p.adapter.FireEventToTools("Log.entryAdded", consoleMessage)
	return nil
}

func (p *ProtocolAdapter) enumerateStyleSheets(message []byte) []byte {
	p.adapter.CallTarget("CSS.getAllStyleSheets", map[string]interface{}{}, func(message []byte) {
		msg := string(message)
		var err error
		headers := gjson.Get(string(message), "headers")
		if headers.Exists() {
			for _, header := range headers.Array() {
				msg, err = sjson.Set(msg, header.Get("isInline").Path(msg), false)
				if err != nil {
					log.Panic(err)
				}
				msg, err = sjson.Set(msg, header.Get("startLine").Path(msg), 0)
				if err != nil {
					log.Panic(err)
				}
				msg, err = sjson.Set(msg, header.Get("startColumn").Path(msg), 0)
				if err != nil {
					log.Panic(err)
				}
				p.adapter.FireEventToTools("CSS.styleSheetAdded", map[string]interface{}{
					"header": gjson.Get(msg, header.Path(msg)),
				})
			}
		}
	})
	return nil
}

func (p *ProtocolAdapter) onAddRule(message []byte) []byte {
	var selector = gjson.Get(string(message), "params.ruleText").String()
	selector = strings.TrimSpace(selector)
	selector = strings.Replace(selector, "{}", "", -1)
	params := map[string]interface{}{
		"contextNodeId": p.lastNodeId,
		"selector":      selector,
	}
	p.adapter.CallTarget("CSS.addRule", params, func(message []byte) {
		var msg = string(message)
		var param interface{}
		err := json.Unmarshal(message, param)
		if err != nil {
			log.Panic(err)
		}
		msg = p.mapRule(gjson.Get(msg, "rule"), msg)
		p.adapter.FireResultToTools(int(gjson.Get(msg, "id").Int()), param)
	})
	return nil
}

func (p *ProtocolAdapter) mapRule(cssRule gjson.Result, message string) string {
	var err error
	if cssRule.Get("ruleId").Exists() {
		path := cssRule.Get("styleSheetId").Path(message)
		message, err = sjson.Set(message, path, cssRule.Get("ruleId.styleSheetId").Value())
		if err != nil {
			log.Panic(err)
		}
		message, err = sjson.Delete(message, path)
		if err != nil {
			log.Panic(err)
		}
		// todo
		message = p.mapSelectorList(cssRule.Get("selectorList"), message)

		p.mapStyle(cssRule.Get("style"), cssRule.Get("origin").String(), message)

		path = cssRule.Get("sourceLine").Path(message)
		message, err = sjson.Delete(message, path)
		if err != nil {
			log.Panic(err)
		}
	}
	return message
}

func (p *ProtocolAdapter) onGetMatchedStylesForNodeResult(message []byte) []byte {
	msg := string(message)
	result := gjson.Get(msg, "result")
	if result.Exists() {
		for _, matchedCSSRule := range result.Get("matchedCSSRules").Array() {
			msg = p.mapRule(matchedCSSRule.Get("rule"), msg)
		}
		for _, inherited := range result.Get("inherited").Array() {
			if inherited.Get("matchedCSSRules").Exists() {
				for _, matchedCSSRule := range result.Get("matchedCSSRules").Array() {
					msg = p.mapRule(matchedCSSRule.Get("rule"), msg)
				}
			}
		}
	}
	return []byte(msg)
}

// onSetStyleTexts todo KeyCheck
func (p *ProtocolAdapter) onSetStyleTexts(message []byte) []byte {
	var msg = string(message)
	var allStyleText []interface{}
	resultId := gjson.Get(msg, "id").Int()
	editsResult := gjson.Get(msg, "params.edits").Array()
	var whetherToContinueTheCycle = true

	for _, edit := range editsResult {
		if !whetherToContinueTheCycle {
			break
		}
		paramsGetStyleSheet := map[string]interface{}{
			"styleSheetId": edit.Get("styleSheetId").String(),
		}
		p.adapter.CallTarget("CSS.getStyleSheet", paramsGetStyleSheet, func(message []byte) {
			var result = string(message)
			styleSheet := gjson.Get(result, "styleSheet")
			styleSheetRules := gjson.Get(result, "styleSheet.rules")
			if !styleSheet.Exists() || !styleSheetRules.Exists() {
				log.Panic("iOS returned a value we were not expecting for getStyleSheet")
			}
			for index, rule := range styleSheetRules.Array() {
				if compareRanges(rule.Get("style.range"), edit.Get("range")) {
					params := map[string]interface{}{
						"styleId": map[interface{}]interface{}{
							"styleSheetId": edit.Get("styleSheetId").String(),
							"ordinal":      index,
						},
						"text": edit.Get("text").String(),
					}
					p.adapter.CallTarget("CSS.setStyleText", params, func(setStyleResult []byte) {
						mapStyleResult := p.mapStyle(gjson.Get(string(setStyleResult), "style"), "", string(setStyleResult))
						allStyleText = append(allStyleText, gjson.Get(mapStyleResult, "style").Value())
						// stop for
						whetherToContinueTheCycle = false
					})
				}
			}
		})
	}
	result := map[string]interface{}{
		"styles": allStyleText,
	}
	p.adapter.FireResultToTools(int(resultId), result)
	return nil
}

func (p *ProtocolAdapter) mapStyle(cssStyle gjson.Result, ruleOrigin string, message string) string {
	var err error
	if cssStyle.Get("cssText").Exists() {
		disabled := p.extractDisabledStyles(cssStyle.Get("cssText").String(), cssStyle.Get("range"))
		for i, value := range disabled {
			noSpaceStr := strings.TrimSpace(value.Content)
			// 原版 const text = disabled[i].content.trim().replace(/^\/\*\s*/, '').replace(/;\s*\*\/$/, '');
			reg := regexp.MustCompile(`^\\/\\*\\s*`)
			noSpaceStr = reg.ReplaceAllString(noSpaceStr, ``)

			reg = regexp.MustCompile(`;\\s*\\*\\/$`)
			noSpaceStr = reg.ReplaceAllString(noSpaceStr, ``)

			parts := strings.Split(noSpaceStr, ":")
			if cssStyle.Get("cssProperties").Exists() {
				cssProperties := cssStyle.Get("cssProperties").Array()
				var index = len(cssProperties)
				for j, _ := range cssProperties {
					if cssProperties[j].Get("range").Exists() &&
						(cssProperties[j].Get("range.startLine").Int() > int64(disabled[i].CssRange.StartLine) ||
							cssProperties[j].Get("range.startLine").Int() == int64(disabled[i].CssRange.StartLine) ||
							cssProperties[j].Get("range.startColumn").Int() > int64(disabled[i].CssRange.StartColumn)) {
						index = j
						break
					}
				}

				cssPropertiesObjects := cssStyle.Get("cssProperties").Value()
				path := cssStyle.Get("cssProperties").Path(message)
				// insert index
				if cssPropertiesArrays, ok := cssPropertiesObjects.([]interface{}); ok {
					var cssPropertiesFinal []interface{}
					cssPropertiesLeft := cssPropertiesArrays[:index+1]
					cssPropertiesRight := cssPropertiesArrays[index+1:]

					cssPropertiesFinal = append(cssPropertiesLeft, map[string]interface{}{
						"implicit": false,
						"name":     parts[0],
						"range":    disabled[i].CssRange,
						"status":   "disabled",
						"text":     disabled[i].Content,
						"value":    parts[1],
					})
					cssPropertiesFinal = append(cssPropertiesFinal, cssPropertiesRight...)
					arr, err1 := json.Marshal(cssPropertiesFinal)
					if err1 != nil {
						log.Panic(err1)
					}
					message, err = sjson.Set(message, path, string(arr))
					if err != nil {
						log.Panic(err)
					}
				} else {
					log.Panic(fmt.Errorf("failed to convert object"))
				}
			}
		}
	}

	for _, cssProperty := range gjson.Get(message, cssStyle.Get("cssProperties").Path(message)).Array() {
		message = p.mapCssProperty(cssProperty, message)
	}
	if ruleOrigin != "user-agent" {
		path := cssStyle.Get("styleSheetId").Path(message)
		message, err = sjson.Set(message, path, cssStyle.Get("styleId.styleSheetId").String())
		if err != nil {
			log.Panic(err)
		}
		cssStyleRangeArr, err1 := json.Marshal(cssStyle.Get("range").Value())
		if err1 != nil {
			log.Panic(err1)
		}
		var styleKey = fmt.Sprintf("%s_%s", cssStyle.Get("styleId.styleSheetId").String(), string(cssStyleRangeArr))
		if p.styleMap == nil {
			p.styleMap = make(map[string]interface{})

		}
		p.styleMap[styleKey] = cssStyle.Get("styleId.styleSheetId").String()
		// delete
		path = cssStyle.Get("styleId").Path(message)
		message, err = sjson.Delete(message, path)
		if err != nil {
			log.Panic(err)
		}
		path = cssStyle.Get("sourceLine").Path(message)
		message, err = sjson.Delete(message, path)
		if err != nil {
			log.Panic(err)
		}
		path = cssStyle.Get("sourceURL").Path(message)
		message, err = sjson.Delete(message, path)
		if err != nil {
			log.Panic(err)
		}
		path = cssStyle.Get("width").Path(message)
		message, err = sjson.Delete(message, path)
		if err != nil {
			log.Panic(err)
		}
		path = cssStyle.Get("height").Path(message)
		message, err = sjson.Delete(message, path)
		if err != nil {
			log.Panic(err)
		}
	}
	return message
}

func (p *ProtocolAdapter) mapCssProperty(cssProperty gjson.Result, message string) string {
	var err error
	path := cssProperty.Get("status.disabled").Path(message)
	if cssProperty.Get("status").String() == "disabled" {
		message, err = sjson.Set(message, path, true)
		if err != nil {
			log.Panic(err)
		}
	} else if cssProperty.Get("status").String() == "active" {
		message, err = sjson.Set(message, path, false)
		if err != nil {
			log.Panic(err)
		}
	}
	// delete cssProperty.status;
	path = cssProperty.Get("status").Path(message)
	message, err = sjson.Delete(message, path)
	if err != nil {
		log.Panic(err)
	}

	priority := cssProperty.Get("priority")
	if priority.Exists() && priority.String() != "" {
		message, err = sjson.Set(message, cssProperty.Get("important").Path(message), true)
	} else {
		message, err = sjson.Set(message, cssProperty.Get("important").Path(message), false)
	}
	if err != nil {
		log.Panic(err)
	}

	path = priority.Path(message)
	message, err = sjson.Delete(message, path)
	if err != nil {
		log.Panic(err)
	}
	return message
}

// extractDisabledStyles todo KeyCheck
func (p *ProtocolAdapter) extractDisabledStyles(styleText string, cssRange gjson.Result) []entity.IDisabledStyle {
	var startIndices []int
	var styles []entity.IDisabledStyle
	for index, _ := range styleText {
		endIndexBEGINCOMMENT := index + len(BEGIN_COMMENT)
		endIndexENDCOMMENT := index + len(END_COMMENT)
		if endIndexBEGINCOMMENT <= len(styleText) && string([]rune(styleText)[index:endIndexBEGINCOMMENT]) == BEGIN_COMMENT {
			startIndices = append(startIndices, index)
			index = index + len(BEGIN_COMMENT)
		} else if endIndexENDCOMMENT <= len(styleText) && string([]rune(styleText)[index:endIndexENDCOMMENT]) == END_COMMENT {
			if len(startIndices) == 0 {
				return []entity.IDisabledStyle{}
			}
			startIndex := startIndices[0]
			startIndices = startIndices[1:]
			endIndex := index + len(END_COMMENT)

			startRangeLine, startRangeColumn := p.getLineColumnFromIndex(styleText, startIndex, cssRange)
			endRangeLine, endRangeColumn := p.getLineColumnFromIndex(styleText, endIndex, cssRange)

			propertyItem := entity.IDisabledStyle{
				Content: styleText[startIndex:endIndex],
				CssRange: entity.IRange{
					StartLine:   startRangeLine,
					StartColumn: startRangeColumn,
					EndLine:     endRangeLine,
					EndColumn:   endRangeColumn,
				},
			}
			styles = append(styles, propertyItem)
			index = endIndex - 1
		}
	}
	if len(startIndices) == 0 {
		return []entity.IDisabledStyle{}
	}
	return styles
}

// todo KeyCheck
func (p *ProtocolAdapter) getLineColumnFromIndex(text string, index int, startRange gjson.Result) (line int, column int) {
	if text == "" || index < 0 || index > len(text) {
		return 0, 0
	}
	if startRange.Exists() {
		line = int(startRange.Get("StartLine").Int())
		column = int(startRange.Get("StartColumn").Int())
	}
	for i := 0; i < len(text) && i < index; i++ {
		if text[i] == '\r' && i+1 < len(text) && text[i+1] == '\n' {
			i++
			line++
			column = 0
		} else if text[i] == '\n' || text[i] == '\r' {
			line++
			column = 0
		} else {
			column++
		}
	}
	return line, column
}

func compareRanges(rangeLeft gjson.Result, rangeRight gjson.Result) bool {
	return rangeLeft.Get("startLine").Int() == rangeRight.Get("startLine").Int() &&
		rangeLeft.Get("startColumn").Int() == rangeRight.Get("startColumn").Int() &&
		rangeLeft.Get("endLine").Int() == rangeRight.Get("endLine").Int() &&
		rangeLeft.Get("endColumn").Int() == rangeRight.Get("endColumn").Int()
}

func ReplaceMethodNameAndOutputBinary(message []byte, method string) []byte {
	var msg = make(map[string]interface{})
	err := json.Unmarshal(message, &msg)
	if err != nil {
		log.Panic(err)
	}
	// todo Regular?
	msg["method"] = method

	arr, err1 := json.Marshal(message)
	if err1 != nil {
		log.Panic(err1)
	}
	return arr
}

var BEGIN_COMMENT = "/* "
var END_COMMENT = " */"
var SEPARATOR = ": "