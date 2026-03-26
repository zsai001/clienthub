import Foundation
import CLibClientHub

/// Swift wrapper around the C shared library (libclienthub).
/// All C string results are freed after conversion to Swift String.
final class LibClientHub {
    static let shared = LibClientHub()

    private init() {
        CHubInit()
    }

    deinit {
        CHubFree()
    }

    // MARK: - Callbacks

    func setLogCallback(_ callback: @escaping (Int32, String) -> Void) {
        _logCallback = callback
        CHubSetLogCallback { level, msg in
            guard let msg = msg else { return }
            let str = String(cString: msg)
            DispatchQueue.main.async {
                _logCallback?(level, str)
            }
        }
    }

    func setStatusCallback(_ callback: @escaping (Int32, Int32, String) -> Void) {
        _statusCallback = callback
        CHubSetStatusCallback { handle, status, detail in
            guard let detail = detail else { return }
            let str = String(cString: detail)
            DispatchQueue.main.async {
                _statusCallback?(handle, status, str)
            }
        }
    }

    func setEventCallback(_ callback: @escaping (Int32, String) -> Void) {
        _eventCallback = callback
        CHubSetEventCallback { handle, eventJSON in
            guard let eventJSON = eventJSON else { return }
            let str = String(cString: eventJSON)
            DispatchQueue.main.async {
                _eventCallback?(handle, str)
            }
        }
    }

    // MARK: - Client

    func clientCreate(configJSON: String) -> Int32 {
        return withCString(configJSON) { ptr in
            CHubClientCreate(UnsafeMutablePointer(mutating: ptr))
        }
    }

    func clientStart(handle: Int32) {
        CHubClientStart(handle)
    }

    func clientStop(handle: Int32) {
        CHubClientStop(handle)
    }

    func clientDestroy(handle: Int32) {
        CHubClientDestroy(handle)
    }

    func clientGetStatus(handle: Int32) -> [String: Any]? {
        guard let cStr = CHubClientGetStatus(handle) else { return nil }
        defer { CHubFreeString(cStr) }
        return parseJSON(String(cString: cStr))
    }

    // MARK: - Manager

    func managerCreate(addr: String, secret: String) -> Int32 {
        return withCString(addr) { a in
            withCString(secret) { s in
                CHubManagerCreate(
                    UnsafeMutablePointer(mutating: a),
                    UnsafeMutablePointer(mutating: s)
                )
            }
        }
    }

    func managerListClients(handle: Int32) -> [String: Any]? {
        guard let cStr = CHubManagerListClients(handle) else { return nil }
        defer { CHubFreeString(cStr) }
        return parseJSON(String(cString: cStr))
    }

    func managerListTunnels(handle: Int32) -> [String: Any]? {
        guard let cStr = CHubManagerListTunnels(handle) else { return nil }
        defer { CHubFreeString(cStr) }
        return parseJSON(String(cString: cStr))
    }

    func managerListForwards(handle: Int32) -> [String: Any]? {
        guard let cStr = CHubManagerListForwards(handle) else { return nil }
        defer { CHubFreeString(cStr) }
        return parseJSON(String(cString: cStr))
    }

    func managerAddForward(handle: Int32, paramsJSON: String) -> [String: Any]? {
        return withCString(paramsJSON) { ptr in
            guard let cStr = CHubManagerAddForward(handle, UnsafeMutablePointer(mutating: ptr)) else { return nil }
            defer { CHubFreeString(cStr) }
            return parseJSON(String(cString: cStr))
        }
    }

    func managerRemoveForward(handle: Int32, paramsJSON: String) -> [String: Any]? {
        return withCString(paramsJSON) { ptr in
            guard let cStr = CHubManagerRemoveForward(handle, UnsafeMutablePointer(mutating: ptr)) else { return nil }
            defer { CHubFreeString(cStr) }
            return parseJSON(String(cString: cStr))
        }
    }

    func managerKickClient(handle: Int32, name: String) -> [String: Any]? {
        return withCString(name) { ptr in
            guard let cStr = CHubManagerKickClient(handle, UnsafeMutablePointer(mutating: ptr)) else { return nil }
            defer { CHubFreeString(cStr) }
            return parseJSON(String(cString: cStr))
        }
    }

    func managerStatus(handle: Int32) -> [String: Any]? {
        guard let cStr = CHubManagerStatus(handle) else { return nil }
        defer { CHubFreeString(cStr) }
        return parseJSON(String(cString: cStr))
    }

    func managerListExpose(handle: Int32, clientName: String) -> [String: Any]? {
        return withCString(clientName) { ptr in
            guard let cStr = CHubManagerListExpose(handle, UnsafeMutablePointer(mutating: ptr)) else { return nil }
            defer { CHubFreeString(cStr) }
            return parseJSON(String(cString: cStr))
        }
    }

    func managerAddExpose(handle: Int32, paramsJSON: String) -> [String: Any]? {
        return withCString(paramsJSON) { ptr in
            guard let cStr = CHubManagerAddExpose(handle, UnsafeMutablePointer(mutating: ptr)) else { return nil }
            defer { CHubFreeString(cStr) }
            return parseJSON(String(cString: cStr))
        }
    }

    func managerRemoveExpose(handle: Int32, paramsJSON: String) -> [String: Any]? {
        return withCString(paramsJSON) { ptr in
            guard let cStr = CHubManagerRemoveExpose(handle, UnsafeMutablePointer(mutating: ptr)) else { return nil }
            defer { CHubFreeString(cStr) }
            return parseJSON(String(cString: cStr))
        }
    }

    func managerDestroy(handle: Int32) {
        CHubManagerDestroy(handle)
    }

    // MARK: - Helpers

    private func parseJSON(_ str: String) -> [String: Any]? {
        guard let data = str.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            return nil
        }
        return obj
    }

    private func withCString<R>(_ str: String, body: (UnsafePointer<CChar>) -> R) -> R {
        return str.withCString(body)
    }
}

// Global callback storage (must be at file scope for C function pointer compatibility)
private var _logCallback: ((Int32, String) -> Void)?
private var _statusCallback: ((Int32, Int32, String) -> Void)?
private var _eventCallback: ((Int32, String) -> Void)?
