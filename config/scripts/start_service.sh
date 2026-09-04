sudo systemctl daemon-reload
sudo systemctl enable simlive-planner.service
sudo systemctl enable simlive-engines.service
sudo systemctl enable simlive-api.service
sudo systemctl start simlive-planner.service
sudo systemctl start simlive-engines.service
sudo systemctl start simlive-api.service