# WAPulse — Client Onboarding Checklist

Use one copy of this checklist per client. It covers everything needed to
get a client from "signed up" to "receiving live WhatsApp messages through
WAPulse," per the Coexistence-first setup in the System Design Document v4.

---

## 1. Client Intake

- [ ] Client type confirmed: Doctor / Wholesaler / other
- [ ] Legal business name (must exactly match whatever verification
      document is used in step 3 — this is the #1 cause of rejection)
- [ ] Business address
- [ ] Business contact email (a company-domain email speeds things up if
      the client has one; a personal email is fine otherwise)
- [ ] Existing WhatsApp Business app number confirmed with client — will
      this number be used via **Coexistence** (default), or does the
      client want a **new dedicated number** instead? (§2.3 of Design Doc v4)
  - [ ] If new number: SIM/eSIM obtained, capable of receiving SMS/voice OTP

## 2. Business Verification Document — Fallback Order

Collect **one** of the following, in this order of preference:

- [ ] **GST certificate** (fastest path) — PDF/JPG/PNG, GSTIN active,
      name and address match what will be entered in Meta Business Manager
- [ ] **UDYAM (Udyog Aadhaar) registration** — if no GST, register this
      first (free, online, same-day, needs only the owner's Aadhaar + PAN)
- [ ] **Utility bill showing business address** — last resort only; flag
      to client that this may take longer to review

> No website is required for verification — don't let a client without
> one think they're blocked here.

## 3. Meta Business Portfolio

- [ ] Business Portfolio created at business.facebook.com under the
      client's business name
- [ ] Confirmation email sent/received and confirmed
- [ ] 2FA enabled on the account

## 4. WhatsApp Business Account (WABA) Setup

- [ ] WABA created inside the client's Business Portfolio
- [ ] Business display name, category, and support contact set
- [ ] **Coexistence path:** Embedded Signup run, "connect an existing
      number" selected, client authorizes from their phone
  - [ ] Confirmed with client: they understand they must open the
        WhatsApp Business app at least once every 14 days
  - [ ] Confirmed with client: broadcast lists, view-once messages, and
        edit/delete of sent messages will be unavailable once active
- [ ] **New-number path (fallback only):** number registered, confirmed
      not already active on WhatsApp, OTP verification completed

## 5. Business Verification Submission

- [ ] Document from step 2 uploaded in Meta Business Manager
- [ ] Business name/address cross-checked against the document one more
      time before submitting
- [ ] Verification submitted
- [ ] Verification result received: Approved / Rejected (if rejected,
      note reason and correct before resubmitting)

## 6. Credentials → WAPulse App

- [ ] `waba_id` obtained
- [ ] `phone_number_id` obtained
- [ ] Long-lived system-user access token generated
- [ ] Credentials entered into WAPulse's WABA Setup screen
- [ ] Inline test-send from WAPulse succeeded

## 7. Message Templates

- [ ] Default templates for this client type reviewed with the client
      (wording, tone, language) and adjusted if needed
- [ ] Templates submitted to Meta for approval via WAPulse's Template
      Editor
- [ ] Approval status confirmed as `APPROVED` for every template this
      client needs before go-live (bill notice / medicine summary / visit
      reminder, as applicable)

## 8. Folder & Rule Configuration

- [ ] Watched folder(s) configured in WAPulse to match where the client
      actually saves their files
- [ ] For doctor clients: default visit-reminder interval confirmed with
      client (e.g. 7 or 15 days) and set in Scheduled Rules
- [ ] Desktop notification preferences confirmed

## 9. App Setup on Client's Machine

- [ ] WAPulse installed
- [ ] Autostart on login enabled
- [ ] System tray icon confirmed visible and showing "connected" status
- [ ] One real end-to-end test performed: a real file dropped in the
      watched folder, message confirmed received on a test phone

## 10. Go-Live

- [ ] Client briefed on: what triggers a message, what the desktop tray
      icon states mean, and who to contact if something looks wrong
- [ ] Client informed of the 14-day app check-in requirement (Coexistence
      only)
- [ ] Onboarding date and this checklist filed for reference
