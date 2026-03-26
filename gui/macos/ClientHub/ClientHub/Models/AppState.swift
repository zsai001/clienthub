import Foundation
import SwiftUI

/// Shared app state that holds both client and manager state.
@MainActor
class AppState: ObservableObject {
    @Published var connectionState = ConnectionState()
    @Published var serverManager = ServerManager()
    @Published var logMessages: [LogMessage] = []

    private let lib = LibClientHub.shared
    private var callbacksRegistered = false

    init() {
        registerCallbacks()
    }

    private func registerCallbacks() {
        guard !callbacksRegistered else { return }
        callbacksRegistered = true

        lib.setLogCallback { [weak self] level, msg in
            guard let self else { return }
            let entry = LogMessage(
                timestamp: Date(),
                level: LogLevel(rawValue: level) ?? .debug,
                message: msg
            )
            self.logMessages.append(entry)
            if self.logMessages.count > 1000 {
                self.logMessages.removeFirst(self.logMessages.count - 1000)
            }
        }

        lib.setStatusCallback { [weak self] handle, status, detail in
            guard let self else { return }
            if handle == self.connectionState.clientHandle {
                self.connectionState.status = ConnectionStatus(rawValue: status) ?? .disconnected
                self.connectionState.statusDetail = detail
            }
        }
    }
}

struct LogMessage: Identifiable {
    let id = UUID()
    let timestamp: Date
    let level: LogLevel
    let message: String
}

enum LogLevel: Int32 {
    case debug = 0
    case info = 1
    case warn = 2
    case error = 3

    var label: String {
        switch self {
        case .debug: return "DEBUG"
        case .info:  return "INFO"
        case .warn:  return "WARN"
        case .error: return "ERROR"
        }
    }

    var color: Color {
        switch self {
        case .debug: return .secondary
        case .info:  return .primary
        case .warn:  return .orange
        case .error: return .red
        }
    }
}
