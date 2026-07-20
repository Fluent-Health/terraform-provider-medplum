# Import by Bot id. The membership is discovered automatically and source_hash
# is recomputed from the deployed code, so a matching local bundle yields a
# clean no-op plan.
terraform import medplum_bot.subscription_handler 00000000-0000-0000-0000-000000000000
