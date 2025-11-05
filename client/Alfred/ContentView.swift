import SwiftUI

struct ContentView: View {
    @State private var textInput = ""
    @State private var response = ""
    @State private var isWaiting = false

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Alfred")
                .font(.title)
                .fontWeight(.bold)

            Text("Personal AI Butler")
                .font(.caption)
                .foregroundColor(.secondary)

            Divider()

            ScrollView {
                Text(response)
                    .font(.body)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(8)
                    .background(Color.gray.opacity(0.1))
                    .cornerRadius(8)
            }
            .frame(maxHeight: 200)

            VStack(alignment: .trailing) {
                TextEditor(text: $textInput)
                    .frame(minHeight: 80)
                    .overlay(
                        RoundedRectangle(cornerRadius: 8)
                            .stroke(Color.gray.opacity(0.3), lineWidth: 1)
                    )

                Button("Send") {
                    sendMessage()
                }
                .disabled(textInput.isEmpty || isWaiting)
                .padding(.top, 8)
            }

            Spacer()
        }
        .padding()
        .frame(width: 300, height: 400)
    }

    private func sendMessage() {
        guard !textInput.isEmpty else { return }

        isWaiting = true
        let message = textInput
        textInput = ""
        response = "Thinking..."

        // TODO: Implement actual Talker bridge
        DispatchQueue.main.asyncAfter(deadline: .now() + 1) {
            response = "You said: \(message)\n\nThis is a placeholder response. Talker integration coming soon."
            isWaiting = false
        }
    }
}