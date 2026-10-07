import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class Service(
    val id: Long = 0,
    val name: String,
    @SerialName("duration_min") val durationMin: Int,
    val price: String,
    val active: Boolean = true,
)

@Serializable
data class Hours(val weekday: Int, val opens: String, val closes: String)

@Serializable
data class PublicBusiness(
    val name: String,
    val slug: String,
    val timezone: String,
    val services: List<Service>,
    val hours: List<Hours>,
    @SerialName("max_days_ahead") val maxDaysAhead: Int,
    @SerialName("accepting_bookings") val acceptingBookings: Boolean = true,
    val currency: String = "GBP",
)

@Serializable
data class Slots(val date: String, val timezone: String, val slots: List<String>)

@Serializable
data class Appointment(
    val id: Long,
    @SerialName("service_id") val serviceId: Long,
    @SerialName("service_name") val serviceName: String,
    @SerialName("customer_name") val customerName: String,
    @SerialName("customer_phone") val customerPhone: String,
    @SerialName("starts_at") val startsAt: String,
    @SerialName("ends_at") val endsAt: String,
    val status: String,
    @SerialName("whatsapp_opt_in") val whatsappOptIn: Boolean,
    val source: String = "online",
    val notes: String = "",
)

@Serializable
data class BusinessRef(val name: String, val slug: String, val timezone: String)

@Serializable
data class BookingResult(
    val appointment: Appointment,
    val business: BusinessRef,
    @SerialName("manage_token") val manageToken: String,
    @SerialName("manage_url") val manageUrl: String,
)

@Serializable
data class ManagedBooking(
    val appointment: Appointment,
    val business: BusinessRef,
    @SerialName("can_cancel") val canCancel: Boolean,
)

@Serializable
data class Business(
    val id: Long,
    val name: String,
    val slug: String,
    val timezone: String,
    @SerialName("google_review_url") val googleReviewUrl: String,
    @SerialName("owner_email") val ownerEmail: String,
    val currency: String = "GBP",
    @SerialName("email_verified") val emailVerified: Boolean = true,
)

@Serializable
data class DayAppointments(val date: String, val appointments: List<Appointment>)

@Serializable
data class Message(
    val type: String,
    val status: String,
    @SerialName("send_at") val sendAt: String,
    @SerialName("sent_at") val sentAt: String? = null,
    val attempts: Int,
    @SerialName("last_error") val lastError: String? = null,
)

@Serializable
data class Stats(
    val done: Int,
    @SerialName("no_show") val noShow: Int,
    val cancelled: Int,
    val unmarked: Int,
    @SerialName("reminders_sent") val remindersSent: Int,
    @SerialName("reviews_sent") val reviewsSent: Int,
)

@Serializable
data class StatsResponse(val days: Int, val stats: Stats)

@Serializable
data class BookingRequest(
    @SerialName("service_id") val serviceId: Long,
    @SerialName("starts_at") val startsAt: String,
    @SerialName("customer_name") val customerName: String,
    @SerialName("customer_phone") val customerPhone: String,
    @SerialName("whatsapp_opt_in") val whatsappOptIn: Boolean,
    val notes: String = "",
)

@Serializable
data class SignupRequest(
    @SerialName("business_name") val businessName: String,
    val slug: String,
    val timezone: String,
    val email: String,
    val password: String,
)

@Serializable
data class LoginRequest(val email: String, val password: String)

@Serializable
data class BusinessUpdate(
    val name: String,
    val timezone: String,
    @SerialName("google_review_url") val googleReviewUrl: String,
    val currency: String,
)

@Serializable
data class StatusRequest(val status: String)

@Serializable
data class ApiError(val error: String)

@Serializable
data class AppConfig(
    @SerialName("company_name") val companyName: String = "Bookly",
    @SerialName("support_email") val supportEmail: String = "",
    @SerialName("billing_enabled") val billingEnabled: Boolean = false,
    @SerialName("price_label") val priceLabel: String = "",
    @SerialName("trial_days") val trialDays: Int = 14,
)

@Serializable
data class Billing(
    val enabled: Boolean,
    val status: String,
    val active: Boolean,
    @SerialName("trial_ends_at") val trialEndsAt: String,
    @SerialName("current_period_end") val currentPeriodEnd: String? = null,
    @SerialName("has_customer") val hasCustomer: Boolean,
    @SerialName("price_label") val priceLabel: String = "",
)

@Serializable
data class RedirectUrl(val url: String)

@Serializable
data class MessageResponse(val message: String)

@Serializable
data class ForgotRequest(val email: String)

@Serializable
data class ResetRequest(val token: String, val password: String)

@Serializable
data class ChangePasswordRequest(
    @SerialName("current_password") val currentPassword: String,
    @SerialName("new_password") val newPassword: String,
)

@Serializable
data class DeleteAccountRequest(val password: String)

@Serializable
data class OwnerBookingResult(val appointment: Appointment)

@Serializable
data class RescheduleRequest(@SerialName("starts_at") val startsAt: String)

@Serializable
data class TimeOff(
    val id: Long = 0,
    @SerialName("starts_at") val startsAt: String,
    @SerialName("ends_at") val endsAt: String,
    val reason: String = "",
)

@Serializable
data class TimeOffResult(
    @SerialName("time_off") val timeOff: TimeOff,
    @SerialName("clashing_appointments") val clashingAppointments: Int,
)

@Serializable
data class TokenRequest(val token: String)

@Serializable
data class ChangeEmailRequest(val email: String, val password: String)
