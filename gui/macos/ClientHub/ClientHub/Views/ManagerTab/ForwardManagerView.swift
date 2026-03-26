import SwiftUI

struct ForwardManagerView: View {
    @EnvironmentObject var appState: AppState

    @State private var showAddSheet = false
    @State private var selection: ForwardInfoItem.ID?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text("Managed Forwards")
                    .font(.subheadline.bold())
                Spacer()

                if let sel = selection,
                   let fwd = appState.serverManager.forwards.first(where: { $0.id == sel }) {
                    Button("Remove") {
                        appState.serverManager.removeForward(from: fwd.clientName, listen: fwd.listenAddr)
                        selection = nil
                    }
                    .tint(.red)
                    .controlSize(.small)
                }

                Button {
                    showAddSheet = true
                } label: {
                    Image(systemName: "plus")
                }
                .buttonStyle(.borderless)
                .controlSize(.small)

                Button {
                    appState.serverManager.fetchForwards()
                } label: {
                    Image(systemName: "arrow.clockwise")
                }
                .buttonStyle(.borderless)
                .controlSize(.small)
            }
            .padding(.horizontal)
            .padding(.vertical, 6)

            if appState.serverManager.forwards.isEmpty {
                Text("No active forwards")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .center)
                    .padding()
            } else {
                Table(appState.serverManager.forwards, selection: $selection) {
                    TableColumn("Client", value: \.clientName)
                        .width(min: 80, ideal: 100)
                    TableColumn("Listen", value: \.listenAddr)
                        .width(min: 80, ideal: 100)
                    TableColumn("Target", value: \.remoteClient)
                        .width(min: 80, ideal: 100)
                    TableColumn("Service", value: \.remoteService)
                        .width(min: 60, ideal: 80)
                    TableColumn("Proto", value: \.protocol)
                        .width(50)
                }
            }
        }
        .sheet(isPresented: $showAddSheet) {
            AddManagedForwardSheet { from, listen, to, service, proto in
                appState.serverManager.addForward(from: from, listen: listen, to: to, service: service, proto: proto)
            }
        }
    }
}

struct AddManagedForwardSheet: View {
    @Environment(\.dismiss) var dismiss
    @State private var fromClient = ""
    @State private var listenAddr = ""
    @State private var toClient = ""
    @State private var toService = ""
    @State private var proto = "tcp"

    var onAdd: (String, String, String, String, String) -> Void

    var body: some View {
        VStack(spacing: 16) {
            Text("Add Forward")
                .font(.headline)

            Form {
                TextField("From Client:", text: $fromClient)
                    .help("Source client that will listen")
                TextField("Listen Address:", text: $listenAddr)
                    .help("e.g. :13306")
                TextField("To Client:", text: $toClient)
                    .help("Target client name")
                TextField("Service:", text: $toService)
                    .help("Target service name")
                Picker("Protocol:", selection: $proto) {
                    Text("TCP").tag("tcp")
                    Text("UDP").tag("udp")
                }
                .pickerStyle(.segmented)
            }
            .formStyle(.grouped)

            HStack {
                Button("Cancel") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Spacer()
                Button("Add Forward") {
                    onAdd(fromClient, listenAddr, toClient, toService, proto)
                    dismiss()
                }
                .keyboardShortcut(.defaultAction)
                .disabled(fromClient.isEmpty || listenAddr.isEmpty || toClient.isEmpty || toService.isEmpty)
            }
        }
        .padding()
        .frame(width: 380)
    }
}
