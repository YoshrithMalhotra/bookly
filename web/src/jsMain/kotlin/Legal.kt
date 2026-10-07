import kotlinx.coroutines.launch
import kotlinx.html.*
import org.w3c.dom.HTMLElement

/**
 * Starter Terms and Privacy pages. They are a template, not legal advice:
 * have them reviewed for your country before you charge customers.
 */
class LegalPage(private val root: HTMLElement, private val privacy: Boolean) {
    fun show() {
        setTitle(if (privacy) "Privacy Policy" else "Terms of Service")
        scope.launch {
            val c = Config.get()
            render(c)
        }
    }

    private fun render(c: AppConfig) {
        val company = c.companyName
        val contact = c.supportEmail.ifEmpty { "our support address" }
        root.render {
            div("page") {
                topBar()
                main("container narrow legal") {
                    if (privacy) privacy(company, contact) else terms(company, contact, c)
                }
                siteFooter()
            }
        }
    }

    private fun FlowContent.privacy(company: String, contact: String) {
        h1 { +"Privacy Policy" }
        p { +"This policy explains what personal data $company collects and how it is used." }
        h2 { +"Business owners" }
        p { +"We store your business name, login email, a securely hashed password, your services, opening hours and settings. Payments are handled by Stripe; we never see or store your card details." }
        h2 { +"Customers who book" }
        p {
            +"When you book, we store your name, phone number, the appointment, and whether you agreed to WhatsApp messages. "
            +"The business you booked with can see this information. It is used only to manage your appointment and, if you agreed, "
            +"to send you a reminder and a review request by WhatsApp."
        }
        h2 { +"Who we share data with" }
        ul {
            li { +"Twilio / WhatsApp, to deliver messages you agreed to" }
            li { +"Stripe, to take subscription payments from businesses" }
            li { +"Our hosting and email providers, to run the service" }
        }
        p { +"We do not sell personal data or use it for advertising." }
        h2 { +"How long we keep it" }
        p { +"Data is kept while the business account is open. When a business deletes its account, its services, appointments and customer details are deleted immediately." }
        h2 { +"Your rights" }
        p { +"You can ask for a copy of your data, a correction, or deletion by contacting $contact. Customers can also contact the business they booked with." }
    }

    private fun FlowContent.terms(company: String, contact: String, c: AppConfig) {
        h1 { +"Terms of Service" }
        p { +"These terms apply to businesses using $company to take bookings." }
        h2 { +"The service" }
        p { +"$company provides an online booking page, appointment management, and optional WhatsApp reminders and review requests. We work hard to keep it available but cannot guarantee it will be uninterrupted or that every message is delivered." }
        h2 { +"Your account" }
        p { +"Keep your password safe. You are responsible for the information you add and for having a lawful basis to message your customers." }
        if (c.billingEnabled) {
            h2 { +"Payment" }
            p {
                if (c.trialDays > 0) +"New accounts get a free ${c.trialDays}-day trial. "
                +"After that the service is billed by subscription${if (c.priceLabel.isNotEmpty()) " (" + c.priceLabel + ")" else ""} through Stripe, "
                +"renewing automatically until you cancel. You can cancel any time from the Billing page; access continues to the end of the paid period. "
                +"If payment fails and isn't resolved, your booking page stops accepting new bookings."
            }
        }
        h2 { +"Acceptable use" }
        p { +"Don't use $company to send spam, to message people who didn't agree, or for anything illegal. We may suspend accounts that do." }
        h2 { +"Ending" }
        p { +"You can delete your account at any time in Settings, which deletes your data. We may end the service with reasonable notice." }
        h2 { +"Liability" }
        p { +"To the extent the law allows, our liability is limited to the fees you paid in the last 12 months." }
        h2 { +"Contact" }
        p { +"Questions: $contact." }
    }
}
