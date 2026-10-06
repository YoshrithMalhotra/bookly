import kotlinx.coroutines.launch
import kotlinx.html.*
import org.w3c.dom.HTMLElement

class HomePage(private val root: HTMLElement) {
    private var config = AppConfig()

    fun show() {
        setTitle("")
        render()
        scope.launch {
            config = Config.get()
            render()
        }
    }

    private fun render() {
        root.render {
            div("page") {
                topBar {
                    link("/login", "button ghost", "Log in")
                    link("/signup", "button", "Start free")
                }
                main("container") {
                    section("hero") {
                        h1 { +"Online booking that cuts no-shows." }
                        p("lead") {
                            +"Give your customers a booking link. Bookly sends them a WhatsApp reminder the day before, "
                            +"and asks happy customers for a Google review after their visit."
                        }
                        div("actions") {
                            link("/signup", "button large", "Create your booking page")
                            link("/login", "button ghost large", "I already have an account")
                        }
                    }
                    section("features") {
                        feature("Your own booking page", "Share one link on Instagram, Google Maps or WhatsApp. Customers pick a service and a free time in seconds.")
                        feature("WhatsApp reminders", "A reminder 24 hours before each appointment, with a link to cancel so the slot frees up for someone else.")
                        feature("More Google reviews", "When you mark a visit as done, Bookly asks the customer for a review.")
                        feature("No double bookings", "Bookly checks every booking against your opening hours and calendar.")
                    }
                    if (config.billingEnabled) {
                        section("card pricing") {
                            h2 { +"Simple pricing" }
                            if (config.priceLabel.isNotEmpty()) p("price") { +config.priceLabel }
                            ul {
                                li { +"Unlimited bookings and services" }
                                li { +"WhatsApp reminders and review requests" }
                                li { +"Cancel any time" }
                            }
                            if (config.trialDays > 0) p("muted") { +"Free for ${config.trialDays} days. No card needed to start." }
                            link("/signup", "button", if (config.trialDays > 0) "Start your free trial" else "Get started")
                        }
                    }
                }
                siteFooter { div { +"Built for salons, tutors and clinics." } }
            }
        }
    }

    private fun FlowContent.feature(title: String, text: String) {
        div("card feature") {
            h3 { +title }
            p { +text }
        }
    }
}
