import { buildClient, CommitmentPolicy, KmsKeyringNode } from "@aws-crypto/client-node";
import { PublishCommand, SNSClient } from "@aws-sdk/client-sns";

const { decrypt } = buildClient(CommitmentPolicy.REQUIRE_ENCRYPT_ALLOW_DECRYPT);
const keyring = new KmsKeyringNode({
  generatorKeyId: process.env.KEY_ID,
  keyIds: [process.env.KEY_ARN],
});
const sns = new SNSClient({
  region: process.env.SMS_REGION || process.env.AWS_REGION || "ap-south-1",
});

const signupTriggers = new Set([
  "CustomSMSSender_SignUp",
  "CustomSMSSender_ResendCode",
  "CustomSMSSender_VerifyUserAttribute",
  "CustomSMSSender_UpdateUserAttribute",
]);

function requiredEnv(name) {
  const value = process.env[name];
  if (!value) {
    throw new Error(`missing required environment variable ${name}`);
  }
  return value;
}

function maskPhoneNumber(phoneNumber) {
  if (!phoneNumber || phoneNumber.length < 4) {
    return "****";
  }
  return `${phoneNumber.slice(0, 3)}${"*".repeat(Math.max(phoneNumber.length - 7, 0))}${phoneNumber.slice(-4)}`;
}

function templateIdForTrigger(triggerSource) {
  if (signupTriggers.has(triggerSource)) {
    return requiredEnv("INDIA_SIGNUP_TEMPLATE_ID");
  }
  return requiredEnv("INDIA_AUTH_TEMPLATE_ID");
}

function messageTemplateForTrigger(triggerSource) {
  if (signupTriggers.has(triggerSource)) {
    return requiredEnv("INDIA_SIGNUP_MESSAGE_TEMPLATE");
  }
  return requiredEnv("INDIA_AUTH_MESSAGE_TEMPLATE");
}

function messageForTrigger(triggerSource, code) {
  const template = messageTemplateForTrigger(triggerSource);
  if (!template.includes("{####}")) {
    throw new Error("India SMS message template must include the {####} OTP placeholder");
  }

  return template.replaceAll("{####}", code);
}

async function decryptCode(encodedCiphertext) {
  if (!encodedCiphertext) {
    throw new Error("missing Cognito code payload");
  }

  const { plaintext } = await decrypt(
    keyring,
    Buffer.from(encodedCiphertext, "base64"),
  );

  return Buffer.from(plaintext).toString("utf-8").trim();
}

export const handler = async (event) => {
  const phoneNumber = event?.request?.userAttributes?.phone_number;
  if (!phoneNumber) {
    throw new Error("missing phone number in Cognito custom SMS event");
  }

  const code = await decryptCode(event?.request?.code);
  const message = messageForTrigger(event.triggerSource, code);
  const templateId = templateIdForTrigger(event.triggerSource);

  await sns.send(new PublishCommand({
    PhoneNumber: phoneNumber,
    Message: message,
    MessageAttributes: {
      "AWS.SNS.SMS.SMSType": {
        DataType: "String",
        StringValue: "Transactional",
      },
      "AWS.MM.SMS.SenderID": {
        DataType: "String",
        StringValue: requiredEnv("INDIA_SENDER_ID"),
      },
      "AWS.MM.SMS.EntityId": {
        DataType: "String",
        StringValue: requiredEnv("INDIA_DLT_ENTITY_ID"),
      },
      "AWS.MM.SMS.TemplateId": {
        DataType: "String",
        StringValue: templateId,
      },
    },
  }));

  console.log(JSON.stringify({
    trigger_source: event.triggerSource,
    phone_number: maskPhoneNumber(phoneNumber),
    status: "sent",
  }));
};
