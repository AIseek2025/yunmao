import SwiftUI

public struct LoginView: View {
    @EnvironmentObject var session: SessionStore
    @State private var phone: String = ""
    @State private var code: String = ""
    @State private var error: String?
    @State private var loading: Bool = false

    public init() {}

    public var body: some View {
        NavigationStack {
            VStack(spacing: 16) {
                Text("yunmao 云养猫")
                    .font(.largeTitle)
                    .bold()
                phoneField()
                codeField()
                Button(action: doLogin) {
                    if loading {
                        ProgressView()
                    } else {
                        Text("登录")
                            .frame(maxWidth: .infinity)
                            .padding()
                            .background(Color.accentColor)
                            .foregroundColor(.white)
                            .cornerRadius(8)
                    }
                }
                .disabled(loading || phone.isEmpty || code.isEmpty)
                if let error { Text(error).foregroundColor(.red).font(.footnote) }
                Spacer()
            }
            .padding()
            .navigationTitle("登录")
        }
    }

    @ViewBuilder
    private func phoneField() -> some View {
        #if os(iOS)
        TextField("手机号", text: $phone)
            .keyboardType(.phonePad)
            .textFieldStyle(.roundedBorder)
        #else
        TextField("手机号", text: $phone)
        #endif
    }

    @ViewBuilder
    private func codeField() -> some View {
        #if os(iOS)
        TextField("验证码", text: $code)
            .keyboardType(.numberPad)
            .textFieldStyle(.roundedBorder)
        #else
        TextField("验证码", text: $code)
        #endif
    }

    private func doLogin() {
        loading = true
        Task {
            do {
                try await session.login(phone: phone, code: code)
            } catch {
                self.error = String(describing: error)
            }
            loading = false
        }
    }
}
