import SwiftUI

struct ClientView: View {
    @EnvironmentObject var appState: AppState

    var body: some View {
        HSplitView {
            VStack(spacing: 0) {
                connectionPanel
                Divider()
                serviceAndForwardPanel
            }
            .frame(minWidth: 400)

            logPanel
                .frame(minWidth: 280)
        }
    }

    // MARK: - Connection Panel

    private var connectionPanel: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Client Connection")
                    .font(.headline)
                Spacer()
                statusIndicator
            }

            Grid(alignment: .leading, horizontalSpacing: 8, verticalSpacing: 8) {
                GridRow {
                    Text("Server:")
                        .frame(width: 80, alignment: .trailing)
                    TextField("host:port", text: $appState.connectionState.serverAddr)
                        .textFieldStyle(.roundedBorder)
                        .disabled(appState.connectionState.isRunning)
                }
                GridRow {
                    Text("Name:")
                        .frame(width: 80, alignment: .trailing)
                    TextField("client-name", text: $appState.connectionState.clientName)
                        .textFieldStyle(.roundedBorder)
                        .disabled(appState.connectionState.isRunning)
                }
                GridRow {
                    Text("Secret:")
                        .frame(width: 80, alignment: .trailing)
                    SecureField("shared secret", text: $appState.connectionState.secret)
                        .textFieldStyle(.roundedBorder)
                        .disabled(appState.connectionState.isRunning)
                }
            }

            HStack {
                if appState.connectionState.isRunning {
                    Button("Disconnect") {
                        appState.connectionState.disconnect()
                    }
                    .controlSize(.large)
                    .tint(.red)
                } else {
                    Button("Connect") {
                        appState.connectionState.connect()
                    }
                    .controlSize(.large)
                    .disabled(
                        appState.connectionState.serverAddr.isEmpty ||
                        appState.connectionState.clientName.isEmpty ||
                        appState.connectionState.secret.isEmpty
                    )
                }

                Spacer()

                if !appState.connectionState.statusDetail.isEmpty {
                    Text(appState.connectionState.statusDetail)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .padding()
    }

    private var statusIndicator: some View {
        HStack(spacing: 6) {
            Circle()
                .fill(statusColor)
                .frame(width: 10, height: 10)
            Text(appState.connectionState.status.label)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }

    private var statusColor: Color {
        switch appState.connectionState.status {
        case .disconnected: return .red
        case .connecting, .reconnecting: return .orange
        case .connected: return .green
        }
    }

    // MARK: - Services & Forwards

    private var serviceAndForwardPanel: some View {
        VStack(spacing: 0) {
            ServiceListView()
                .environmentObject(appState)
            Divider()
            ForwardListView()
                .environmentObject(appState)
        }
    }

    // MARK: - Log Panel

    private var logPanel: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text("Logs")
                    .font(.headline)
                Spacer()
                Button("Clear") {
                    appState.logMessages.removeAll()
                }
                .buttonStyle(.borderless)
                .font(.caption)
            }
            .padding(.horizontal)
            .padding(.vertical, 8)

            Divider()

            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 2) {
                        ForEach(appState.logMessages) { entry in
                            logRow(entry)
                                .id(entry.id)
                        }
                    }
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                }
                .onChange(of: appState.logMessages.count) { _ in
                    if let last = appState.logMessages.last {
                        proxy.scrollTo(last.id, anchor: .bottom)
                    }
                }
            }
            .background(Color(nsColor: .textBackgroundColor))
        }
    }

    private func logRow(_ entry: LogMessage) -> some View {
        HStack(alignment: .top, spacing: 4) {
            Text(entry.timestamp, format: .dateTime.hour().minute().second())
                .font(.system(.caption2, design: .monospaced))
                .foregroundStyle(.secondary)
            Text(entry.level.label)
                .font(.system(.caption2, design: .monospaced))
                .foregroundStyle(entry.level.color)
                .frame(width: 40, alignment: .leading)
            Text(entry.message)
                .font(.system(.caption2, design: .monospaced))
                .textSelection(.enabled)
        }
    }
}
