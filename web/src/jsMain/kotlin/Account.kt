import kotlinx.coroutines.launch
import kotlinx.html.*
import kotlinx.html.js.onSubmitFunction
import org.w3c.dom.HTMLElement

/** /forgot: ask for a password reset email. */
class ForgotPage(private val root: HTMLElement) {
    private var error: String? = null
    private var sent: String? = null
    private var busy = false

    fun show() {
        setTitle("Reset password")
        render()
    }

    private fun render() {
        root.render {
            div("page") {
                topBar { link("/login", "button ghost", "Log in") }
                main("container narrow") {
                    div("card") {
                        h1 { +"Reset your password" }
                        errorBox(error)
                        val done = sent
                        if (done != null) {
                            okBox(done)
                            p("muted") { +"Check your inbox (and spam folder). The link works for one hour." }
                            return@div
                        }
                        p("muted") { +"Enter your login email and we'll send you a link to choose a new password." }
                        form {
                            onSubmitFunction = { e -> e.stop(); submit() }
                            label {
                                +"Email"
                                input(InputType.email) { id = "email"; required = true; attributes["autocomplete"] = "email" }
                            }
                            button(type = ButtonType.submit, classes = "button full") {
                                disabled = busy
                                +(if (busy) "Sending…" else "Send reset link")
                            }
                        }
                    }
                }
            }
        }
    }

    private fun submit() {
        val email = inputValue("email")
        busy = true; error = null; render()
        scope.launch {
            try {
                sent = Api.send<ForgotRequest, MessageResponse>("POST", "/api/password/forgot", ForgotRequest(email)).message
            } catch (e: ApiException) {
                error = e.message
            }
            busy = false
            render()
        }
    }
}

/** /reset/{token}: choose a new password from the emailed link. */
class ResetPage(private val root: HTMLElement, private val token: String) {
    private var error: String? = null
    private var busy = false

    fun show() {
        setTitle("Choose a new password")
        render()
    }

    private fun render() {
        root.render {
            div("page") {
                topBar()
                main("container narrow") {
                    div("card") {
                        h1 { +"Choose a new password" }
                        errorBox(error)
                        form {
                            onSubmitFunction = { e -> e.stop(); submit() }
                            label {
                                +"New password"
                                input(InputType.password) {
                                    id = "password"; required = true; minLength = "10"; maxLength = "72"
                                    attributes["autocomplete"] = "new-password"
                                }
                                small("muted") { +"At least 10 characters. You'll be logged out everywhere else." }
                            }
                            label {
                                +"Repeat new password"
                                input(InputType.password) { id = "password2"; required = true; attributes["autocomplete"] = "new-password" }
                            }
                            button(type = ButtonType.submit, classes = "button full") {
                                disabled = busy
                                +(if (busy) "Saving…" else "Save password and log in")
                            }
                        }
                        p("muted") { link("/forgot", null, "Need a new link?") }
                    }
                }
            }
        }
    }

    private fun submit() {
        val pw = rawInputValue("password")
        if (pw != rawInputValue("password2")) {
            error = "The two passwords don't match."; render(); return
        }
        busy = true; error = null; render()
        scope.launch {
            try {
                Api.send<ResetRequest, Business>("POST", "/api/password/reset", ResetRequest(token, pw))
                Router.go("/owner", replace = true)
            } catch (e: ApiException) {
                busy = false; error = e.message; render()
            }
        }
    }
}
