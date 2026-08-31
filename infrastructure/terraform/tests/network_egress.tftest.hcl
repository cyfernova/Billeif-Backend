mock_provider "aws" {
  override_during = plan

  mock_data "aws_ami" {
    defaults = {
      id = "ami-0billeifnat"
    }
  }

  mock_data "aws_iam_policy_document" {
    override_during = plan
    defaults = {
      json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}"
    }
  }

  mock_resource "aws_lambda_invocation" {
    defaults = {
      result = "{\"status\":\"applied\",\"version\":53,\"latest_version\":53,\"dirty\":false,\"manifest_checksum\":\"e6b4bef1df19096e4ab93d3aec502445f0ba0c25af134cb2b2bef09b8aa1af9f\"}"
    }
  }

  override_data {
    target = data.aws_caller_identity.current
    values = {
      account_id = "928282274753"
      arn        = "arn:aws:iam::928282274753:user/terraform-test"
      user_id    = "AIDATEST1234567890"
    }
  }

  override_data {
    target = data.aws_partition.current
    values = {
      partition = "aws"
    }
  }

  override_resource {
    target          = aws_api_gateway_rest_api.main
    override_during = plan
    values = {
      id               = "test-rest-api"
      root_resource_id = "test-root-resource"
      execution_arn    = "arn:aws:execute-api:ap-south-1:928282274753:test-rest-api"
    }
  }

  override_resource {
    target          = aws_kms_key.application_secrets
    override_during = plan
    values = {
      arn    = "arn:aws:kms:ap-south-1:928282274753:key/application-secrets"
      key_id = "application-secrets"
    }
  }

  override_resource {
    target          = aws_db_instance.main
    override_during = plan
    values = {
      address = "database.internal"
      master_user_secret = [{
        kms_key_id = "arn:aws:kms:ap-south-1:928282274753:key/application-secrets"
        secret_arn = "arn:aws:secretsmanager:ap-south-1:928282274753:secret/rds-managed"
      }]
    }
  }

  override_resource {
    target          = aws_security_group.database_migrator
    override_during = plan
    values = {
      id = "sg-database-migrator"
    }
  }

  override_resource {
    target          = aws_security_group.lambda
    override_during = plan
    values = {
      id = "sg-application-lambda"
    }
  }

  override_resource {
    target          = aws_subnet.private[0]
    override_during = plan
    values = {
      id = "subnet-private-a"
    }
  }

  override_resource {
    target          = aws_subnet.private[1]
    override_during = plan
    values = {
      id = "subnet-private-b"
    }
  }

  override_resource {
    target          = aws_subnet.public[0]
    override_during = plan
    values = {
      id = "subnet-public-a"
    }
  }

  override_resource {
    target          = aws_subnet.public[1]
    override_during = plan
    values = {
      id = "subnet-public-b"
    }
  }

  override_resource {
    target          = aws_route_table.private
    override_during = plan
    values = {
      id = "rtb-private"
    }
  }

  override_resource {
    target          = aws_route_table.database
    override_during = plan
    values = {
      id = "rtb-database-isolated"
    }
  }

  override_resource {
    target          = aws_instance.nat[0]
    override_during = plan
    values = {
      id                           = "i-billeifnat"
      primary_network_interface_id = "eni-billeifnat"
    }
  }
}

mock_provider "aws" {
  alias           = "ap_south_1"
  override_during = plan
}

mock_provider "aws" {
  alias           = "us_east_1"
  override_during = plan
}

mock_provider "awscc" {
  override_during = plan
}

variables {
  project_name                   = "billeif-test"
  environment                    = "test"
  lambda_artifact_dir            = "tests/fixtures/lambda"
  migration_lambda_artifact_path = "tests/fixtures/lambda/http.zip"
  llm_api_url                    = "https://llm.example.test/chat/completions"
  llm_model                      = "test-model"
  deepseek_base_url              = "https://voice-llm.example.test/v1"
  deepseek_model                 = "voice-test-model"
  ses_verified_identity          = "billeif.example"
  ses_sender_email               = "notifications@billeif.example"
  db_allowed_cidr                = "10.0.0.0/24"
}

run "nat_instance_is_the_cost_capped_default" {
  command = plan

  override_resource {
    target          = aws_eip.nat_instance[0]
    override_during = plan
    values = {
      id = "eipalloc-billeif-nat-instance"
    }
  }

  assert {
    condition = (
      var.egress_mode == "nat_instance" &&
      length(aws_instance.nat) == 1 &&
      length(aws_nat_gateway.main) == 0 &&
      length(aws_eip.nat_instance) == 1 &&
      length(aws_eip.nat) == 0 &&
      length(aws_eip_association.nat_instance) == 1 &&
      length(aws_ssm_association.nat_bootstrap_ready) == 1 &&
      length(aws_ssm_association.nat_activation_ready) == 0 &&
      length(aws_ec2_instance_state.nat_running) == 0 &&
      length(aws_ec2_instance_state.nat_stopped) == 1 &&
      length(aws_instance.rds_tunnel) == 0 &&
      aws_ec2_instance_state.nat_stopped[0].state == "stopped" &&
      aws_eip_association.nat_instance[0].allocation_id == aws_eip.nat_instance[0].id &&
      aws_route.private_default_egress.network_interface_id == aws_instance.nat[0].primary_network_interface_id &&
      aws_route.private_default_egress.nat_gateway_id == null
    )
    error_message = "Billeif private egress must default to one NAT instance with managed NAT disabled."
  }

  assert {
    condition = (
      aws_instance.nat[0].instance_type == "t4g.micro" &&
      aws_instance.nat[0].ami == data.aws_ami.billeif_nat_instance[0].id &&
      aws_instance.nat[0].source_dest_check == false &&
      aws_instance.nat[0].subnet_id == aws_subnet.public[1].id &&
      aws_instance.nat[0].associate_public_ip_address == false &&
      aws_instance.nat[0].user_data_replace_on_change == false &&
      aws_instance.nat[0].monitoring == false &&
      aws_instance.nat[0].metadata_options[0].http_endpoint == "enabled" &&
      aws_instance.nat[0].metadata_options[0].http_tokens == "required" &&
      aws_instance.nat[0].credit_specification[0].cpu_credits == "standard" &&
      aws_instance.nat[0].root_block_device[0].encrypted == true &&
      aws_instance.nat[0].root_block_device[0].volume_type == "gp3" &&
      aws_instance.nat[0].tags.BilleifNatTarget == local.nat_instance_readiness_target
    )
    error_message = "The Billeif NAT instance must be hardened Arm64 t4g.micro compute with encrypted gp3 and standard CPU credits."
  }

  assert {
    condition = alltrue([
      for subnet in aws_subnet.public :
      subnet.map_public_ip_on_launch == false
    ])
    error_message = "Billeif public subnets must not auto-assign public IPv4 addresses; explicitly managed EIPs provide public reachability."
  }

  assert {
    condition = (
      length(aws_security_group.nat_instance[0].ingress) == 1 &&
      alltrue([
        for rule in aws_security_group.nat_instance[0].ingress :
        rule.protocol == "-1" &&
        toset(rule.cidr_blocks) == toset(local.private_subnet_cidrs) &&
        rule.from_port != 22 &&
        rule.to_port != 22
      ]) &&
      length(aws_security_group.nat_instance[0].egress) == 1 &&
      alltrue([
        for rule in aws_security_group.nat_instance[0].egress :
        rule.protocol == "-1" &&
        toset(rule.cidr_blocks) == toset(["0.0.0.0/0"])
      ])
    )
    error_message = "The Billeif NAT security group must allow forwarding only from private subnets, expose no SSH ingress, and allow outbound traffic."
  }

  assert {
    condition = (
      strcontains(aws_instance.nat[0].user_data, "net.ipv4.ip_forward=1") &&
      strcontains(aws_instance.nat[0].user_data, "-t nat -C POSTROUTING") &&
      strcontains(aws_instance.nat[0].user_data, "MASQUERADE") &&
      strcontains(aws_instance.nat[0].user_data, "while iptables -w 5 -C FORWARD") &&
      strcontains(aws_instance.nat[0].user_data, "iptables -w 5 -I FORWARD 1") &&
      strcontains(aws_instance.nat[0].user_data, "iptables-save >/etc/sysconfig/iptables") &&
      strcontains(aws_instance.nat[0].user_data, "for attempt in {1..20}") &&
      strcontains(aws_instance.nat[0].user_data, "systemctl enable --now iptables") &&
      strcontains(aws_instance.nat[0].user_data, "billeif-nat-watchdog.timer") &&
      strcontains(aws_instance.nat[0].user_data, "systemctl enable --now billeif-nat.service billeif-nat-watchdog.timer")
    )
    error_message = "The Billeif NAT bootstrap must persist forwarding and masquerading and enable the systemd watchdog."
  }

  assert {
    condition = (
      aws_iam_role.nat_instance[0].name == "${local.resource_prefix}-nat-instance-role" &&
      aws_iam_instance_profile.nat_instance[0].name == "${local.resource_prefix}-nat-instance-profile" &&
      aws_iam_role_policy_attachment.nat_instance_ssm[0].policy_arn == "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
    )
    error_message = "The Billeif NAT instance must be SSM-managed without SSH."
  }

  assert {
    condition = (
      aws_ssm_association.nat_bootstrap_ready[0].association_name == "${local.resource_prefix}-nat-bootstrap-ready" &&
      aws_ssm_association.nat_bootstrap_ready[0].name == "AWS-RunShellScript" &&
      aws_ssm_association.nat_bootstrap_ready[0].wait_for_success_timeout_seconds == 600 &&
      aws_ssm_association.nat_bootstrap_ready[0].targets[0].key == "tag:BilleifNatTarget" &&
      toset(aws_ssm_association.nat_bootstrap_ready[0].targets[0].values) == toset([local.nat_instance_readiness_target]) &&
      strcontains(aws_ssm_association.nat_bootstrap_ready[0].parameters.commands, "base64 --decode") &&
      strcontains(aws_ssm_association.nat_bootstrap_ready[0].parameters.commands, "/usr/local/sbin/billeif-nat-configure") &&
      strcontains(aws_ssm_association.nat_bootstrap_ready[0].parameters.commands, "systemctl reset-failed billeif-nat.service") &&
      strcontains(aws_ssm_association.nat_bootstrap_ready[0].parameters.commands, "systemctl restart billeif-nat.service") &&
      strcontains(aws_ssm_association.nat_bootstrap_ready[0].parameters.commands, "billeif-nat.service") &&
      strcontains(aws_ssm_association.nat_bootstrap_ready[0].parameters.commands, "net.ipv4.ip_forward")
    )
    error_message = "The Billeif migration must wait at no extra cost for SSM to verify NAT forwarding readiness."
  }
}

run "large_lambda_artifacts_use_versioned_s3_delivery" {
  command = plan

  override_resource {
    target          = aws_s3_bucket.lambda_artifacts
    override_during = plan
    values = {
      id = "billeif-test-lambda-artifacts"
    }
  }

  override_resource {
    target          = aws_s3_object.sqs_invoice_lambda_artifact
    override_during = plan
    values = {
      version_id = "invoice-version"
    }
  }

  override_resource {
    target          = aws_s3_object.sqs_gst_lambda_artifact
    override_during = plan
    values = {
      version_id = "gst-version"
    }
  }

  override_resource {
    target          = aws_s3_object.ws_lambda_artifact
    override_during = plan
    values = {
      version_id = "ws-version"
    }
  }

  override_resource {
    target          = aws_s3_object.custom_sms_sender_lambda_artifact
    override_during = plan
    values = {
      version_id = "custom-sms-version"
    }
  }

  assert {
    condition = alltrue([
      for function in [
        aws_lambda_function.sqs_invoice,
        aws_lambda_function.sqs_gst,
        aws_lambda_function.ws_handler,
        aws_lambda_function.custom_sms_sender,
      ] :
      function.filename == null &&
      function.s3_bucket == aws_s3_bucket.lambda_artifacts.id &&
      function.s3_key != null &&
      function.s3_object_version != null
    ])
    error_message = "Large Billeif Lambda artifacts must be delivered through the private versioned artifact bucket instead of inline CreateFunction uploads."
  }

  assert {
    condition = alltrue([
      for artifact in [
        aws_s3_object.sqs_invoice_lambda_artifact,
        aws_s3_object.sqs_gst_lambda_artifact,
        aws_s3_object.ws_lambda_artifact,
        aws_s3_object.custom_sms_sender_lambda_artifact,
      ] :
      artifact.bucket == aws_s3_bucket.lambda_artifacts.id &&
      artifact.server_side_encryption == "AES256" &&
      startswith(artifact.key, "lambda/")
    ])
    error_message = "Each large Billeif Lambda ZIP must be staged as an encrypted content-addressed S3 object."
  }
}

run "managed_nat_is_an_explicit_opt_in" {
  command = plan

  variables {
    egress_mode = "managed_nat"
  }

  override_resource {
    target          = aws_eip.nat[0]
    override_during = plan
    values = {
      id = "eipalloc-billeif-managed-nat"
    }
  }

  override_resource {
    target          = aws_nat_gateway.main[0]
    override_during = plan
    values = {
      id = "nat-billeif-managed"
    }
  }

  assert {
    condition = (
      length(aws_instance.nat) == 0 &&
      length(aws_nat_gateway.main) == 1 &&
      length(aws_eip.nat_instance) == 0 &&
      length(aws_eip.nat) == 1 &&
      aws_nat_gateway.main[0].allocation_id == aws_eip.nat[0].id &&
      length(aws_eip_association.nat_instance) == 0 &&
      length(aws_ssm_association.nat_bootstrap_ready) == 0 &&
      length(aws_ssm_association.nat_activation_ready) == 0 &&
      length(aws_ec2_instance_state.nat_running) == 0 &&
      length(aws_ec2_instance_state.nat_stopped) == 0 &&
      aws_route.private_default_egress.nat_gateway_id == aws_nat_gateway.main[0].id
    )
    error_message = "Managed NAT must replace, not duplicate, Billeif NAT-instance egress when explicitly selected."
  }
}

run "application_activation_starts_the_nat_instance" {
  command = plan

  variables {
    enable_application                 = true
    enable_lambda_reserved_concurrency = true
    alert_email                        = "alerts@billeif.example"
    alert_email_subscription_confirmed = true
  }

  assert {
    condition = (
      length(aws_ssm_association.nat_bootstrap_ready) == 1 &&
      length(aws_ssm_association.nat_activation_ready) == 1 &&
      aws_ssm_association.nat_activation_ready[0].association_name == "${local.resource_prefix}-nat-activation-ready" &&
      aws_ssm_association.nat_activation_ready[0].wait_for_success_timeout_seconds == 600 &&
      aws_ssm_association.nat_activation_ready[0].targets[0].key == "tag:BilleifNatTarget" &&
      toset(aws_ssm_association.nat_activation_ready[0].targets[0].values) == toset([local.nat_instance_readiness_target]) &&
      strcontains(aws_ssm_association.nat_activation_ready[0].parameters.commands, "base64 --decode") &&
      strcontains(aws_ssm_association.nat_activation_ready[0].parameters.commands, "/usr/local/sbin/billeif-nat-configure") &&
      strcontains(aws_ssm_association.nat_activation_ready[0].parameters.commands, "systemctl reset-failed billeif-nat.service") &&
      strcontains(aws_ssm_association.nat_activation_ready[0].parameters.commands, "systemctl restart billeif-nat.service") &&
      length(aws_ec2_instance_state.nat_running) == 1 &&
      aws_ec2_instance_state.nat_running[0].state == "running" &&
      length(aws_ec2_instance_state.nat_stopped) == 0
    )
    error_message = "Billeif application activation must start the cost-capped NAT instance before runtime traffic is enabled."
  }
}

run "private_gateway_endpoints_and_nat_alarms_are_explicit" {
  command = plan

  assert {
    condition = (
      aws_vpc_endpoint.s3.vpc_endpoint_type == "Gateway" &&
      aws_vpc_endpoint.s3.service_name == "com.amazonaws.ap-south-1.s3" &&
      toset(aws_vpc_endpoint.s3.route_table_ids) == toset([aws_route_table.private.id]) &&
      aws_vpc_endpoint.dynamodb.vpc_endpoint_type == "Gateway" &&
      aws_vpc_endpoint.dynamodb.service_name == "com.amazonaws.ap-south-1.dynamodb" &&
      toset(aws_vpc_endpoint.dynamodb.route_table_ids) == toset([aws_route_table.private.id]) &&
      !contains(aws_vpc_endpoint.s3.route_table_ids, aws_route_table.database.id) &&
      !contains(aws_vpc_endpoint.dynamodb.route_table_ids, aws_route_table.database.id)
    )
    error_message = "Free S3 and DynamoDB gateway endpoints must attach only to the private application route table and leave the database route table isolated."
  }

  assert {
    condition = alltrue([
      for alarm in [
        aws_cloudwatch_metric_alarm.nat_system_status[0],
        aws_cloudwatch_metric_alarm.nat_cpu_high[0],
        aws_cloudwatch_metric_alarm.nat_cpu_credits_low[0]
      ] :
      alarm.evaluation_periods == 3 &&
      alarm.datapoints_to_alarm == 2 &&
      alarm.treat_missing_data == "notBreaching" &&
      alarm.dimensions.InstanceId == aws_instance.nat[0].id
    ])
    error_message = "Billeif NAT status, CPU, and credit alarms must use explicit 2-of-3 evaluation with non-breaching missing data."
  }

  assert {
    condition = (
      aws_cloudwatch_metric_alarm.nat_system_status[0].metric_name == "StatusCheckFailed_System" &&
      aws_cloudwatch_metric_alarm.nat_system_status[0].period == 60 &&
      contains(aws_cloudwatch_metric_alarm.nat_system_status[0].alarm_actions, "arn:aws:automate:ap-south-1:ec2:recover") &&
      aws_cloudwatch_metric_alarm.nat_cpu_high[0].metric_name == "CPUUtilization" &&
      aws_cloudwatch_metric_alarm.nat_cpu_high[0].period == 300 &&
      aws_cloudwatch_metric_alarm.nat_cpu_credits_low[0].metric_name == "CPUCreditBalance" &&
      aws_cloudwatch_metric_alarm.nat_cpu_credits_low[0].period == 300
    )
    error_message = "The Billeif NAT system alarm must recover the instance and the remaining alarms must cover CPU and credit exhaustion."
  }
}

run "egress_mode_rejects_unknown_values" {
  command = plan

  variables {
    egress_mode = "surprise"
  }

  expect_failures = [var.egress_mode]
}
